package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"runtime/debug"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
)

type toolExecResult struct {
	index int
	name  string
	tr    tools.ToolResult
}

type OrchestratorConfig struct {
	MaxConcurrentTools int
	ToolTimeout        time.Duration
	LongRunningTimeout time.Duration
	ZombieTimeout      time.Duration
}

type ToolPipeline interface {
	ExecuteTool(ctx context.Context, call *llm.FunctionCall) tools.ToolResult
	RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool)
	IsSerial(toolName string) bool
}

type defaultToolPipeline struct {
	resolver   ToolResolutionService
	authorizer ToolAuthorizer
	runtime    ToolExecutor
	registry   tools.Registry
}

func (p *defaultToolPipeline) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	return p.authorizer.RequestBatchConsent(ctx, calls)
}

func (p *defaultToolPipeline) IsSerial(toolName string) bool {
	return p.registry.IsSerial(toolName)
}

func (p *defaultToolPipeline) ExecuteTool(parentCtx context.Context, call *llm.FunctionCall) (result tools.ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			result = tools.ToolResult{
				Text:  "Tool \"" + call.Name + "\" encountered an internal fatal error (panic) and was terminated.",
				Error: fmt.Errorf("%w: Panic detected: %v", llm.ErrTerminal, r),
			}
		}
	}()

	tool, err := p.resolver.Resolve(call)
	if err != nil {
		return tools.ToolResult{Text: err.Error(), Error: fmt.Errorf("%w: %v", llm.ErrTerminal, err)}
	}

	result, err = p.runtime.Execute(parentCtx, tool, call, nil)
	status, msg := classifyToolError(err, result.Error)

	if status == "user_declined" || status == "security_blocked" {
		return tools.ToolResult{Text: msg, Error: nil}
	}

	if err != nil {
		if result.Error == nil {
			result.Error = err
		}
		if result.Text == "" {
			result.Text = fmt.Sprintf("Error: %v", err)
		}
		result.Error = fmt.Errorf("%w: %v", llm.ErrTerminal, result.Error)
	}
	return result
}

func NewDefaultToolPipeline(
	registry tools.Registry,
	sm domain_security.Manager,
	bus events.EventBus,
	logger ports.Logger,
	zombie *tools.ZombieTool,
	failures *failureTracker,
	getToolTimeout func() time.Duration,
	getLongRunningTimeout func() time.Duration,
	getZombieTimeout func() time.Duration,
) ToolPipeline {
	resolver := newToolResolutionService(registry)
	authService := newSecurityAuthorizer(sm, registry)

	var exec ToolExecutor = newBaseRuntime(registry)
	exec = newAuthDecorator(exec, authService)
	// Circuit breaker is moved to orchestrator loop
	exec = newTracingDecorator(exec, registry, logger)
	exec = newSafetyDecorator(exec, registry, logger, bus, zombie, getToolTimeout, getLongRunningTimeout, getZombieTimeout)

	return &defaultToolPipeline{
		resolver:   resolver,
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}
}

type orchestratorState struct {
	config OrchestratorConfig
	pool   *concurrency.WorkerPool
}

type Orchestrator struct {
	state    atomic.Pointer[orchestratorState]
	pipeline ToolPipeline
	events   events.EventBus
	logger   ports.Logger
	strategy resultStrategy
	failures *failureTracker
	observer tools.ExecutionObserver
	zombie   *tools.ZombieTool
}

type executorOption func(*Orchestrator)

func WithLongRunningTimeout(timeout time.Duration) executorOption {
	return func(e *Orchestrator) {
		oldState := e.state.Load()
		newState := &orchestratorState{config: oldState.config, pool: oldState.pool}
		newState.config.LongRunningTimeout = timeout
		e.state.Store(newState)
	}
}

func withZombieTimeout(timeout time.Duration) executorOption {
	return func(e *Orchestrator) {
		oldState := e.state.Load()
		newState := &orchestratorState{config: oldState.config, pool: oldState.pool}
		newState.config.ZombieTimeout = timeout
		e.state.Store(newState)
	}
}

func withToolTimeout(timeout time.Duration) executorOption {
	return func(e *Orchestrator) {
		oldState := e.state.Load()
		newState := &orchestratorState{config: oldState.config, pool: oldState.pool}
		newState.config.ToolTimeout = timeout
		e.state.Store(newState)
	}
}

func NewOrchestrator(cfg OrchestratorConfig, pipeline ToolPipeline, bus events.EventBus, logger ports.Logger, observer tools.ExecutionObserver, opts ...executorOption) (*Orchestrator, error) {
	if pipeline == nil {
		return nil, errors.New("pipeline is required")
	}
	if observer == nil {
		return nil, errors.New("ExecutionObserver is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	if cfg.MaxConcurrentTools <= 0 {
		cfg.MaxConcurrentTools = 5
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = 30 * time.Second
	}
	if cfg.LongRunningTimeout <= 0 {
		cfg.LongRunningTimeout = 5 * time.Minute
	}
	if cfg.ZombieTimeout <= 0 {
		cfg.ZombieTimeout = 5 * time.Minute
	}

	e := &Orchestrator{
		pipeline: pipeline,
		events:   bus,
		logger:   logger,
		strategy: &markdownStrategy{},
		failures: newFailureTracker(3),
		observer: observer,
	}

	initialState := &orchestratorState{
		config: cfg,
		pool:   concurrency.NewWorkerPool(cfg.MaxConcurrentTools),
	}
	e.state.Store(initialState)

	for _, opt := range opts {
		opt(e)
	}

	return e, nil
}

type failureTracker struct {
	mu        sync.RWMutex
	failures  map[string]int
	threshold int
}

func newFailureTracker(threshold int) *failureTracker {
	return &failureTracker{
		failures:  make(map[string]int),
		threshold: threshold,
	}
}

func (f *failureTracker) recordFailure(toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[toolName]++
}

func (f *failureTracker) recordSuccess(toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[toolName] = 0
}

func (f *failureTracker) Record(toolName string, success bool) {
	if success {
		f.recordSuccess(toolName)
	} else {
		f.recordFailure(toolName)
	}
}

func (f *failureTracker) isOpen(toolName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.failures[toolName] >= f.threshold
}

func (f *failureTracker) Check(toolName string) error {
	if f.isOpen(toolName) {
		return fmt.Errorf("%w: tool %q is temporarily disabled due to multiple consecutive failures", tools.ErrToolCircuitOpen, toolName)
	}
	return nil
}

func (e *Orchestrator) SetConcurrency(maxConcurrent int, timeout time.Duration) {
	for {
		oldState := e.state.Load()
		newState := &orchestratorState{
			config: oldState.config,
			pool:   oldState.pool,
		}

		changed := false
		if maxConcurrent > 0 && maxConcurrent != oldState.config.MaxConcurrentTools {
			newState.config.MaxConcurrentTools = maxConcurrent
			newState.pool = concurrency.NewWorkerPool(maxConcurrent)
			changed = true
		}
		if timeout > 0 && timeout != oldState.config.ToolTimeout {
			newState.config.ToolTimeout = timeout
			changed = true
		}

		if !changed {
			return
		}

		if e.state.CompareAndSwap(oldState, newState) {
			if oldState.pool != newState.pool && oldState.pool != nil {
				oldState.pool.Shutdown()
			}
			return
		}
	}
}

func (e *Orchestrator) Shutdown() {
	state := e.state.Load()
	if state != nil && state.pool != nil {
		state.pool.Shutdown()
	}
}

func (e *Orchestrator) emitEvent(ctx context.Context, bus events.EventBus, evt events.Event) {
	if err := events.SafePublish(ctx, bus, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			e.logger.Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}

func (e *Orchestrator) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	calls := e.extractFunctionCalls(respContent)
	if len(calls) == 0 {
		return nil, nil
	}

	if turn >= maxToolTurns {
		evt := events.SystemMessageEvent{
			Message: fmt.Sprintf("Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.", maxToolTurns),
			Level:   "error",
		}
		e.emitEvent(ctx, e.events, evt)
		return nil, llm.ErrMaxTurnsReached
	}

	e.emitEvent(ctx, e.events, events.ToolCallEvent{
		Calls:    calls,
		Turn:     turn,
		MaxTurns: maxToolTurns,
	})

	var declinedMap map[int]bool
	func() {
		eventCtx := context.WithoutCancel(ctx)
		e.emitEvent(eventCtx, e.events, events.ConsentStartedEvent{})
		defer e.emitEvent(eventCtx, e.events, events.ConsentFinishedEvent{})
		ctx, declinedMap = e.pipeline.RequestBatchConsent(ctx, calls)
	}()

	startTime := time.Now()

	results := make([]tools.ToolResult, len(calls))
	waitErr := e.runExecutionPlan(ctx, calls, declinedMap, results)

	duration := time.Since(startTime)

	if waitErr != nil {
		e.logger.Debug("Tool execution turn failed or was cancelled",
			"turn", turn,
			"error", waitErr.Error(),
			"duration_ms", duration.Milliseconds(),
		)
	} else {
		e.logger.Debug("Tool execution turn completed",
			"turn", turn,
			"tool_calls", len(calls),
			"duration_ms", duration.Milliseconds(),
		)
	}

	for _, tr := range results {
		if errors.Is(tr.Error, tools.ErrToolCircuitOpen) {
			evt := events.SystemMessageEvent{
				Message: tr.Text,
				Level:   "warn",
			}
			e.emitEvent(ctx, e.events, evt)
		}
	}

	return e.assembleResponse(calls, results), waitErr
}

func (e *Orchestrator) extractFunctionCalls(respContent *llm.Content) []*llm.FunctionCall {
	var functionCalls []*llm.FunctionCall
	for _, part := range respContent.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}
	return functionCalls
}

func (e *Orchestrator) assembleResponse(calls []*llm.FunctionCall, results []tools.ToolResult) *llm.Content {
	var responseParts []*llm.Part
	for i, tr := range results {
		responseParts = append(responseParts, e.strategy.Format(calls[i], tr))
		for _, b := range tr.BinaryData {
			responseParts = append(responseParts, &llm.Part{
				InlineData: &llm.Blob{
					MIMEType: b.MIMEType,
					Data:     b.Data,
				},
			})
		}
	}
	return &llm.Content{
		Role:  "user",
		Parts: responseParts,
	}
}

type taskBatch struct {
	isSerial bool
	tasks    []int
}

func (e *Orchestrator) runExecutionPlan(ctx context.Context, calls []*llm.FunctionCall, declinedMap map[int]bool, results []tools.ToolResult) error {
	batches := e.buildExecutionBatches(calls, declinedMap, results)
	state := e.state.Load()

	for batchIdx, batch := range batches {
		if err := ctx.Err(); err != nil {
			e.logger.Debug("Execution plan interrupted", "reason", "context cancelled", "batch_idx", batchIdx)
			e.failRemainingTasks(batches, batchIdx, -1, calls, results, err, "batch interrupted")
			return err
		}

		batchStart := time.Now()

		resultsCh := make(chan toolExecResult, len(batch.tasks))

		// 1. Fan-out
		if batch.isSerial {
			taskIdx := batch.tasks[0]
			fc := calls[taskIdx]
			if err := e.failures.Check(fc.Name); err != nil {
				resultsCh <- toolExecResult{index: taskIdx, name: fc.Name, tr: tools.ToolResult{Text: err.Error(), Error: err}}
			} else {
				tr := e.pipeline.ExecuteTool(ctx, fc)
				resultsCh <- toolExecResult{index: taskIdx, name: fc.Name, tr: tr}
			}
			close(resultsCh)
		} else {
			var wg sync.WaitGroup
			for _, taskIdx := range batch.tasks {
				i := taskIdx
				fc := calls[i]

				if err := e.failures.Check(fc.Name); err != nil {
					resultsCh <- toolExecResult{index: i, name: fc.Name, tr: tools.ToolResult{Text: err.Error(), Error: err}}
					continue
				}

				wg.Add(1)
				task := func(_ context.Context) {
					defer func() {
						if r := recover(); r != nil {
							err := fmt.Errorf("panic during tool execution: %v", r)
							resultsCh <- toolExecResult{
								index: i,
								name:  fc.Name,
								tr:    tools.ToolResult{Text: err.Error(), Error: err},
							}
						}
					}()
					defer wg.Done()
					if ctx.Err() != nil {
						resultsCh <- toolExecResult{
							index: i,
							name:  fc.Name,
							tr:    tools.ToolResult{Text: "Skipped: Context cancelled"},
						}
						return
					}
					tr := e.pipeline.ExecuteTool(ctx, fc)
					resultsCh <- toolExecResult{index: i, name: fc.Name, tr: tr}
				}

				if err := state.pool.Submit(task); err != nil {
					wg.Done()
					var msg string
					if errors.Is(err, concurrency.ErrPoolSaturated) {
						msg = "Error: Tool execution queue is full (pool saturated). Please try again later."
					} else {
						msg = "Error: Task submission failed (pool closed or context cancelled)"
					}
					resultsCh <- toolExecResult{
						index: i,
						name:  fc.Name,
						tr:    tools.ToolResult{Text: msg, Error: err},
					}
				}
			}

			go func() {
				defer func() {
					if r := recover(); r != nil {
						// Recover, capture stack trace, but we cannot easily route to aggregator since we don't know the exact index.
						// Close the channel to avoid deadlock.
						e.logger.Error("panic in fan-in wait goroutine", "panic", r, "stack", string(debug.Stack()))
					}
					close(resultsCh)
				}()
				wg.Wait()
			}()
		}

		// 2. Fan-in Aggregator Loop (lock-free mutation of failures)
		for res := range resultsCh {
			results[res.index] = res.tr
			e.failures.Record(res.name, res.tr.Error == nil)
			evt := events.ToolResultEvent{Name: res.name, Result: res.tr}
			e.emitEvent(ctx, e.events, evt)
		}

		e.logger.Debug("Batch execution completed",
			"batch_idx", batchIdx,
			"is_serial", batch.isSerial,
			"task_count", len(batch.tasks),
			"duration_ms", time.Since(batchStart).Milliseconds())

		// Serial halt logic
		if batch.isSerial {
			if results[batch.tasks[0]].Error != nil || ctx.Err() != nil {
				e.logger.Debug("Serial batch failed or interrupted, halting execution plan",
					"batch_idx", batchIdx,
					"tool_name", calls[batch.tasks[0]].Name)
				e.failRemainingTasks(batches, batchIdx, batch.tasks[0], calls, results, nil, "Skipped: Execution halted due to previous serial tool error, timeout or cancellation.")
				return ctx.Err()
			}
		}
	}
	return ctx.Err()
}

func (e *Orchestrator) failRemainingTasks(batches []taskBatch, startBatchIdx int, skipTaskIdx int, calls []*llm.FunctionCall, results []tools.ToolResult, err error, reason string) {
	for j := startBatchIdx; j < len(batches); j++ {
		for _, skippedIdx := range batches[j].tasks {
			if j == startBatchIdx && skippedIdx <= skipTaskIdx {
				continue
			}

			var text string
			var resErr error
			if err != nil {
				text = fmt.Sprintf("%s: %v", reason, err)
				resErr = fmt.Errorf("%s: %w", reason, err)
			} else {
				text = reason
				resErr = nil
			}

			results[skippedIdx] = tools.ToolResult{
				Text:  text,
				Error: resErr,
			}
		}
	}
}

func (e *Orchestrator) buildExecutionBatches(calls []*llm.FunctionCall, declinedMap map[int]bool, results []tools.ToolResult) []taskBatch {
	var batches []taskBatch
	var currentParallelBatch []int

	for i, fc := range calls {
		if declinedMap[i] {
			results[i] = tools.ToolResult{
				Text:  "User explicitly denied this action.",
				Error: tools.ErrUserDeclined,
			}
			continue
		}

		if e.pipeline.IsSerial(fc.Name) {
			if len(currentParallelBatch) > 0 {
				batches = append(batches, taskBatch{
					isSerial: false,
					tasks:    currentParallelBatch,
				})
				currentParallelBatch = nil
			}
			batches = append(batches, taskBatch{
				isSerial: true,
				tasks:    []int{i},
			})
		} else {
			currentParallelBatch = append(currentParallelBatch, i)
		}
	}

	if len(currentParallelBatch) > 0 {
		batches = append(batches, taskBatch{
			isSerial: false,
			tasks:    currentParallelBatch,
		})
	}

	return batches
}

func buildFunctionResponse(callID, name, output string) *llm.Part {
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			ID:       callID,
			Name:     name,
			Response: map[string]interface{}{"result": output},
		},
	}
}
