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
)

type toolExecResult struct {
	index int
	name  string
	tr    tools.ToolResult
}

type dispatcherConfig struct {
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
				Error: fmt.Errorf("%w: tool execution panic: %v", llm.ErrTerminal, r),
			}
		}
	}()

	tool, err := p.resolver.Resolve(call)
	if err != nil {
		return tools.ToolResult{Text: err.Error(), Error: err}
	}

	result, err = p.runtime.Execute(parentCtx, tool, call, nil)
	status, msg := classifyToolError(err, result.Error)

	switch status {
	case "user_declined":
		return tools.ToolResult{Text: msg, Error: nil}
	case "security_blocked":
		// Identify the actual security error
		secErr := err
		if secErr == nil {
			secErr = result.Error
		}
		if secErr == nil {
			secErr = tools.ErrSecurityPolicy // fallback
		}
		// Return the security error without wrapping as terminal.
		// The LLM will see the message and can adjust its behavior.
		return tools.ToolResult{
			Text:  msg,
			Error: secErr, // NOT wrapped with llm.ErrTerminal
		}
	}

	if err != nil {
		if result.Error == nil {
			result.Error = err
		}
		if result.Text == "" {
			result.Text = fmt.Sprintf("Error: tool execution failed: %s: %v", call.Name, err)
		}
		// Do not wrap in llm.ErrTerminal so the LLM can see the error and retry.
	}
	return result
}

func newDefaultToolPipeline(
	registry tools.Registry,
	sm domain_security.Manager,
	bus events.EventBus,
	logger ports.Logger,
	zombie *tools.ZombieTool,
	toolTimeout time.Duration,
	longRunningTimeout time.Duration,
	zombieTimeout time.Duration,
) ToolPipeline {
	resolver := newToolResolutionService(registry)
	authService := newSecurityAuthorizer(sm, registry)

	var exec ToolExecutor = newBaseRuntime(registry)
	exec = newAuthDecorator(exec, authService)
	exec = newTracingDecorator(exec, registry, logger)
	exec = newSafetyDecorator(exec, registry, logger, bus, zombie, toolTimeout, longRunningTimeout, zombieTimeout)

	return &defaultToolPipeline{
		resolver:   resolver,
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}
}

type dispatcherState struct {
	config dispatcherConfig
}

type Dispatcher struct {
	state    atomic.Pointer[dispatcherState]
	pipeline ToolPipeline
	events   events.EventBus
	logger   ports.Logger
	strategy resultStrategy
	observer tools.ExecutionObserver
	zombie   *tools.ZombieTool
}

type ExecutorOption func(*dispatcherConfig)

func WithLongRunningTimeout(timeout time.Duration) ExecutorOption {
	return func(cfg *dispatcherConfig) {
		cfg.LongRunningTimeout = timeout
	}
}

func withZombieTimeout(timeout time.Duration) ExecutorOption {
	return func(cfg *dispatcherConfig) {
		cfg.ZombieTimeout = timeout
	}
}

func WithToolTimeout(timeout time.Duration) ExecutorOption {
	return func(cfg *dispatcherConfig) {
		cfg.ToolTimeout = timeout
	}
}

func (c *dispatcherConfig) applyDefaults() {
	if c.MaxConcurrentTools <= 0 {
		c.MaxConcurrentTools = 5
	}
	if c.ToolTimeout <= 0 {
		c.ToolTimeout = 30 * time.Second
	}
	if c.LongRunningTimeout <= 0 {
		c.LongRunningTimeout = 5 * time.Minute
	}
	if c.ZombieTimeout <= 0 {
		c.ZombieTimeout = 5 * time.Minute
	}
}

func validateDispatcherDeps(pipeline ToolPipeline, logger ports.Logger, observer tools.ExecutionObserver) error {
	if pipeline == nil {
		return errors.New("pipeline is required")
	}
	if observer == nil {
		return errors.New("execution observer is required")
	}
	if logger == nil {
		return errors.New("logger is required")
	}
	return nil
}

func newDispatcher(cfg dispatcherConfig, pipeline ToolPipeline, bus events.EventBus, logger ports.Logger, observer tools.ExecutionObserver, opts ...ExecutorOption) (*Dispatcher, error) {
	if err := validateDispatcherDeps(pipeline, logger, observer); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	cfg.applyDefaults()

	e := &Dispatcher{
		pipeline: pipeline,
		events:   bus,
		logger:   logger,
		strategy: &markdownStrategy{},

		observer: observer,
	}

	initialState := &dispatcherState{
		config: cfg,
	}
	e.state.Store(initialState)

	return e, nil
}

func (e *Dispatcher) SetConcurrency(maxConcurrent int) {
	for {
		oldState := e.state.Load()
		newState := &dispatcherState{
			config: oldState.config,
		}

		changed := false
		if maxConcurrent > 0 && maxConcurrent != oldState.config.MaxConcurrentTools {
			newState.config.MaxConcurrentTools = maxConcurrent
			changed = true
		}

		if !changed {
			return
		}

		if e.state.CompareAndSwap(oldState, newState) {
			return
		}
	}
}

func (e *Dispatcher) emitEvent(ctx context.Context, bus events.EventBus, evt events.Event) {
	if err := events.SafePublish(ctx, bus, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			e.logger.Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}

func (e *Dispatcher) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
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
		if ctx.Err() != nil {
			return e.AssembleResponse(calls, results), ctx.Err()
		}

		if errors.Is(waitErr, llm.ErrTerminal) {
			return e.AssembleResponse(calls, results), waitErr
		}

		waitErr = nil
	} else {
		e.logger.Debug("Tool execution turn completed",
			"turn", turn,
			"tool_calls", len(calls),
			"duration_ms", duration.Milliseconds(),
		)
	}

	return e.AssembleResponse(calls, results), waitErr
}

func (e *Dispatcher) extractFunctionCalls(respContent *llm.Content) []*llm.FunctionCall {
	var functionCalls []*llm.FunctionCall
	for _, part := range respContent.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}
	return functionCalls
}

func (e *Dispatcher) AssembleResponse(calls []*llm.FunctionCall, results []tools.ToolResult) *llm.Content {
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

func SuggestTool(hallucinated string, validTools []string) string {
	return suggestTool(hallucinated, validTools)
}

type taskBatch struct {
	isSerial bool
	tasks    []int
}

func (e *Dispatcher) runExecutionPlan(ctx context.Context, calls []*llm.FunctionCall, declinedMap map[int]bool, results []tools.ToolResult) error {
	batches := e.buildExecutionBatches(calls, declinedMap, results)
	state := e.state.Load()

	var planErrors []error

	for batchIdx, batch := range batches {
		if err := e.checkPreconditions(ctx, batchIdx, batches, calls, results); err != nil {
			planErrors = append(planErrors, err)
			return errors.Join(planErrors...)
		}

		batchStart := time.Now()
		resultsCh := make(chan toolExecResult, len(batch.tasks))

		// 1. Fan-out
		e.executeBatch(ctx, batch, calls, state.config.MaxConcurrentTools, resultsCh)

		// 2. Fan-in Aggregator Loop (lock-free mutation of failures)
		planErrors = e.handleBatchResults(ctx, resultsCh, results, planErrors)

		e.logger.Debug("Batch execution completed",
			"batch_idx", batchIdx,
			"is_serial", batch.isSerial,
			"task_count", len(batch.tasks),
			"duration_ms", time.Since(batchStart).Milliseconds())

		// Serial halt logic
		halt, haltErr := e.evaluateBatchOutcome(ctx, batchIdx, batch, batches, calls, results)
		if haltErr != nil {
			planErrors = append(planErrors, haltErr)
		}
		if halt {
			if len(planErrors) > 0 {
				return errors.Join(planErrors...)
			}
			return ctx.Err()
		}
	}

	if ctx.Err() != nil {
		planErrors = append(planErrors, ctx.Err())
	}

	if len(planErrors) > 0 {
		return errors.Join(planErrors...)
	}

	return ctx.Err()
}

func (e *Dispatcher) checkPreconditions(ctx context.Context, batchIdx int, batches []taskBatch, calls []*llm.FunctionCall, results []tools.ToolResult) error {
	if err := ctx.Err(); err != nil {
		e.logger.Debug("Execution plan interrupted", "reason", "context cancelled", "batch_idx", batchIdx)
		e.failRemainingTasks(batches, batchIdx, -1, calls, results, err, "batch interrupted")
		return err
	}
	return nil
}

func (e *Dispatcher) executeBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, maxWorkers int, resultsCh chan<- toolExecResult) {
	if batch.isSerial {
		e.executeSerialBatch(ctx, batch, calls, resultsCh)
	} else {
		e.executeParallelBatch(ctx, batch, calls, maxWorkers, resultsCh)
	}
}

func (e *Dispatcher) executeSerialBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, resultsCh chan<- toolExecResult) {
	taskIdx := batch.tasks[0]
	fc := calls[taskIdx]

	var tr tools.ToolResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("panic in serial execution", "panic", r, "stack", string(debug.Stack()))
				err := fmt.Errorf("%w: tool execution panic: %v", llm.ErrTerminal, r)
				tr = tools.ToolResult{Text: err.Error(), Error: err}
			}
		}()
		tr = e.pipeline.ExecuteTool(ctx, fc)
	}()

	resultsCh <- toolExecResult{index: taskIdx, name: fc.Name, tr: tr}
	close(resultsCh)
}

func (e *Dispatcher) executeParallelBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, maxWorkers int, resultsCh chan<- toolExecResult) {
	jobsCh := make(chan int, len(batch.tasks))
	for _, taskIdx := range batch.tasks {
		jobsCh <- taskIdx
	}
	close(jobsCh)

	numWorkers := maxWorkers
	if len(batch.tasks) < numWorkers {
		numWorkers = len(batch.tasks)
	}

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go e.parallelWorker(ctx, calls, jobsCh, resultsCh, &wg)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("panic in fan-in wait goroutine", "panic", r, "stack", string(debug.Stack()))
			}
			close(resultsCh)
		}()
		wg.Wait()
	}()
}

func (e *Dispatcher) parallelWorker(ctx context.Context, calls []*llm.FunctionCall, jobsCh <-chan int, resultsCh chan<- toolExecResult, wg *sync.WaitGroup) {
	defer wg.Done()
	var currentJobIdx = -1
	var currentJobName string

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("panic in worker goroutine", "panic", r, "stack", string(debug.Stack()))
			if currentJobIdx != -1 {
				err := fmt.Errorf("%w: tool execution panic: %v", llm.ErrTerminal, r)
				resultsCh <- toolExecResult{
					index: currentJobIdx,
					name:  currentJobName,
					tr:    tools.ToolResult{Text: err.Error(), Error: err},
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			resultsCh <- toolExecResult{
				index: -1,
				name:  "context_cancelled",
				tr:    tools.ToolResult{Text: "skipped: context cancelled", Error: ctx.Err()},
			}
			return // Graceful exit on cancellation
		case i, ok := <-jobsCh:
			if !ok {
				return
			}
			currentJobIdx = i
			fc := calls[i]
			currentJobName = fc.Name

			tr := e.pipeline.ExecuteTool(ctx, fc)
			resultsCh <- toolExecResult{index: i, name: fc.Name, tr: tr}

			currentJobIdx = -1
			currentJobName = ""
		}
	}
}

func (e *Dispatcher) handleBatchResults(ctx context.Context, resultsCh <-chan toolExecResult, results []tools.ToolResult, planErrors []error) []error {
	for res := range resultsCh {
		if res.index == -1 {
			// cancellation signal
			continue
		}
		results[res.index] = res.tr
		evt := events.ToolResultEvent{Name: res.name, Result: res.tr}
		e.emitEvent(ctx, e.events, evt)

		if res.tr.Error != nil {
			planErrors = append(planErrors, res.tr.Error)
		}
	}
	return planErrors
}

func (e *Dispatcher) evaluateBatchOutcome(ctx context.Context, batchIdx int, batch taskBatch, batches []taskBatch, calls []*llm.FunctionCall, results []tools.ToolResult) (bool, error) {
	if batch.isSerial {
		if results[batch.tasks[0]].Error != nil || ctx.Err() != nil {
			e.logger.Debug("Serial batch failed or interrupted, halting execution plan",
				"batch_idx", batchIdx,
				"tool_name", calls[batch.tasks[0]].Name)
			e.failRemainingTasks(batches, batchIdx, batch.tasks[0], calls, results, nil, "skipped: execution halted due to previous serial tool error, timeout or cancellation")

			if ctx.Err() != nil {
				return true, ctx.Err()
			}
			return true, nil
		}
	} else {
		if err := ctx.Err(); err != nil {
			e.logger.Debug("Parallel batch interrupted, halting execution plan", "batch_idx", batchIdx)
			e.failRemainingTasks(batches, batchIdx, -1, calls, results, err, "batch interrupted")
			return true, err
		}
	}
	return false, nil
}

func (e *Dispatcher) failRemainingTasks(batches []taskBatch, startBatchIdx int, skipTaskIdx int, calls []*llm.FunctionCall, results []tools.ToolResult, err error, reason string) {
	for j := startBatchIdx; j < len(batches); j++ {
		for _, skippedIdx := range batches[j].tasks {
			if j == startBatchIdx && skippedIdx <= skipTaskIdx {
				continue
			}

			if results[skippedIdx].Text != "" || results[skippedIdx].Error != nil {
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

func (e *Dispatcher) buildExecutionBatches(calls []*llm.FunctionCall, declinedMap map[int]bool, results []tools.ToolResult) []taskBatch {
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
