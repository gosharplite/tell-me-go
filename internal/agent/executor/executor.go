// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
	"golang.org/x/sync/errgroup"
)

type toolExecResult struct {
	index int
	name  string
	tr    tools.ToolResult
}

// Orchestrator handles the execution of tools, using a WorkerPool for concurrency.
type Orchestrator struct {
	mu                 sync.RWMutex
	registry           tools.Registry
	authorizer         ToolAuthorizer
	events             events.EventBus
	logger             ports.Logger
	maxConcurrentTools int
	toolTimeout        time.Duration
	longRunningTimeout time.Duration
	zombieTimeout      time.Duration
	pool               *concurrency.WorkerPool
	strategy           resultStrategy
	failures           *failureTracker
	observer           tools.ExecutionObserver
	zombie             *tools.ZombieTool

	resolver ToolResolutionService
	runtime  ToolExecutor
}

// executorOption allows configuring the Orchestrator.
type executorOption func(*Orchestrator)

// WithLongRunningTimeout sets the timeout for long-running tools.
func WithLongRunningTimeout(timeout time.Duration) executorOption {
	return func(e *Orchestrator) {
		e.longRunningTimeout = timeout
	}
}

// withZombieTimeout sets the timeout for zombie tool detection.
func withZombieTimeout(timeout time.Duration) executorOption {
	return func(e *Orchestrator) {
		e.zombieTimeout = timeout
	}
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(registry tools.Registry, sm domain_security.Manager, bus events.EventBus, logger ports.Logger, observer tools.ExecutionObserver, opts ...executorOption) (*Orchestrator, error) {
	if registry == nil {
		return nil, errors.New("registry is required")
	}
	if observer == nil {
		return nil, errors.New("ExecutionObserver is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	e := &Orchestrator{
		registry:           registry,
		events:             bus,
		logger:             logger,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		longRunningTimeout: 5 * time.Minute,
		zombieTimeout:      5 * time.Minute,
		pool:               concurrency.NewWorkerPool(5),
		strategy:           &markdownStrategy{},
		failures:           newFailureTracker(3), // Default threshold of 3
		observer:           observer,
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.zombie == nil {
		var err error
		e.zombie, err = tools.NewZombieTool(e.observer)
		if err != nil {
			e.Shutdown()
			return nil, err
		}
	}

	e.resolver = newToolResolutionService(registry)
	authService := newSecurityAuthorizer(sm, registry)
	e.authorizer = authService // Still used for Batch Consent

	// Wire the ToolExecutor chain
	var exec ToolExecutor = newBaseRuntime(registry)
	exec = newAuthDecorator(exec, authService)
	exec = newCircuitBreakerDecorator(exec, e.failures)
	exec = newTracingDecorator(exec, registry, logger)

	// Use functions to provide dynamic timeouts from the Orchestrator
	getToolTimeout := func() time.Duration {
		e.mu.RLock()
		defer e.mu.RUnlock()
		return e.toolTimeout
	}
	getLongRunningTimeout := func() time.Duration {
		e.mu.RLock()
		defer e.mu.RUnlock()
		return e.longRunningTimeout
	}
	getZombieTimeout := func() time.Duration {
		e.mu.RLock()
		defer e.mu.RUnlock()
		return e.zombieTimeout
	}

	exec = newSafetyDecorator(exec, registry, logger, bus, e.zombie, getToolTimeout, getLongRunningTimeout, getZombieTimeout)

	e.runtime = exec

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
	var oldPool *concurrency.WorkerPool
	e.mu.Lock()

	if maxConcurrent > 0 && maxConcurrent != e.maxConcurrentTools {
		e.maxConcurrentTools = maxConcurrent
		if e.pool != nil {
			oldPool = e.pool
		}
		e.pool = concurrency.NewWorkerPool(maxConcurrent)
	}
	if timeout > 0 {
		e.toolTimeout = timeout
	}
	e.mu.Unlock()

	if oldPool != nil {
		oldPool.Shutdown()
	}
}

// Shutdown shuts down the internal worker pool.
func (e *Orchestrator) Shutdown() {
	e.mu.Lock()
	pool := e.pool
	e.mu.Unlock()

	if pool != nil {
		pool.Shutdown()
	}
}

type resultCollector struct {
	executor *Orchestrator
	calls    []*llm.FunctionCall
	bus      events.EventBus
	trs      []tools.ToolResult
	ch       chan toolExecResult
}

func (e *Orchestrator) newResultCollector(calls []*llm.FunctionCall, bus events.EventBus) *resultCollector {
	return &resultCollector{
		executor: e,
		calls:    calls,
		bus:      bus,
		trs:      make([]tools.ToolResult, len(calls)),
		ch:       make(chan toolExecResult, len(calls)),
	}
}

func (c *resultCollector) Wait(ctx context.Context) ([]tools.ToolResult, error) {
	completedCount := 0
	isCompleted := make([]bool, len(c.calls))
	for completedCount < len(c.calls) {
		select {
		case <-ctx.Done():
			for i := range c.trs {
				if !isCompleted[i] {
					c.trs[i] = tools.ToolResult{Text: "Execution was interrupted or cancelled by the user."}
				}
			}
			return c.trs, ctx.Err()
		case res := <-c.ch:
			if !isCompleted[res.index] {
				c.trs[res.index] = res.tr
				isCompleted[res.index] = true
				evt := events.ToolResultEvent{Name: res.name, Result: res.tr}
				c.executor.emitEvent(ctx, c.bus, evt)
				completedCount++
			}
		}
	}
	return c.trs, nil
}

// emitEvent consolidates error handling for event publishing.
func (e *Orchestrator) emitEvent(ctx context.Context, bus events.EventBus, evt events.Event) {
	if err := events.SafePublish(ctx, bus, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			e.logger.Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}

// Execute handles the execution of function calls from the model response.
func (e *Orchestrator) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	calls := e.extractFunctionCalls(respContent)
	if len(calls) == 0 {
		return nil, nil
	}

	if turn >= maxToolTurns {
		e.publishLimitError(ctx, maxToolTurns)
		return nil, llm.ErrMaxTurnsReached
	}

	e.publishCallEvent(ctx, calls, turn, maxToolTurns)

	e.mu.RLock()
	bus := e.events
	auth := e.authorizer
	e.mu.RUnlock()

	ctx, declinedMap := auth.RequestBatchConsent(ctx, calls)

	// Orchestrate Execution
	collector := e.newResultCollector(calls, bus)
	startTime := time.Now()

	// [SCALABILITY FIX] Bounding the execution plan goroutine to prevent leaks on context cancellation.
	// This ensures that all goroutines started by the plan are properly joined.
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if testExecutionPlanFn != nil {
			return testExecutionPlanFn(e, gCtx, calls, collector.ch, declinedMap)
		}
		return e.runExecutionPlan(gCtx, calls, collector.ch, declinedMap)
	})

	results, waitErr := collector.Wait(gCtx)
	if err := g.Wait(); err != nil && waitErr == nil {
		waitErr = err
	}

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

	// Notify about circuit breaker events
	for _, tr := range results {
		if errors.Is(tr.Error, tools.ErrToolCircuitOpen) {
			e.mu.RLock()
			bus := e.events
			e.mu.RUnlock()
			evt := events.SystemMessageEvent{
				Message: tr.Text,
				Level:   "warn",
			}
			e.emitEvent(ctx, bus, evt)
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

func (e *Orchestrator) publishLimitError(ctx context.Context, maxToolTurns int) {
	e.mu.RLock()
	bus := e.events
	e.mu.RUnlock()
	evt := events.SystemMessageEvent{
		Message: fmt.Sprintf("Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.", maxToolTurns),
		Level:   "error",
	}
	e.emitEvent(ctx, bus, evt)
}

func (e *Orchestrator) publishCallEvent(ctx context.Context, calls []*llm.FunctionCall, turn int, maxToolTurns int) {
	e.mu.RLock()
	bus := e.events
	e.mu.RUnlock()
	evt := events.ToolCallEvent{
		Calls:    calls,
		Turn:     turn,
		MaxTurns: maxToolTurns,
	}
	e.emitEvent(ctx, bus, evt)
}

func (e *Orchestrator) assembleResponse(calls []*llm.FunctionCall, results []tools.ToolResult) *llm.Content {
	e.mu.RLock()
	strategy := e.strategy
	e.mu.RUnlock()

	var responseParts []*llm.Part
	for i, tr := range results {
		responseParts = append(responseParts, strategy.Format(calls[i], tr))
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
	tasks    []int // Contains indices into the 'calls' slice
}

func (e *Orchestrator) runExecutionPlan(ctx context.Context, calls []*llm.FunctionCall, resChan chan<- toolExecResult, declinedMap map[int]bool) error {
	batches := e.buildExecutionBatches(calls, declinedMap, resChan)

	for batchIdx, batch := range batches {
		if err := ctx.Err(); err != nil {
			e.logger.Debug("Execution plan interrupted", "reason", "context cancelled", "batch_idx", batchIdx)
			e.failRemainingTasks(batches, batchIdx, -1, calls, resChan, err, "batch interrupted")
			return err
		}

		batchStart := time.Now()
		if batch.isSerial {
			if !e.executeSerialBatch(ctx, batch, calls, resChan) {
				e.logger.Debug("Serial batch failed or interrupted, halting execution plan",
					"batch_idx", batchIdx,
					"tool_name", calls[batch.tasks[0]].Name)
				e.failRemainingTasks(batches, batchIdx, batch.tasks[0], calls, resChan, nil, "Skipped: Execution halted due to previous serial tool error, timeout or cancellation.")
				return nil // Exit the execution plan early
			}
		} else {
			e.executeParallelBatch(ctx, batch, calls, resChan)
		}

		e.logger.Debug("Batch execution completed",
			"batch_idx", batchIdx,
			"is_serial", batch.isSerial,
			"task_count", len(batch.tasks),
			"duration_ms", time.Since(batchStart).Milliseconds())
	}
	return nil
}

func (e *Orchestrator) failRemainingTasks(batches []taskBatch, startBatchIdx int, skipTaskIdx int, calls []*llm.FunctionCall, resChan chan<- toolExecResult, err error, reason string) {
	for j := startBatchIdx; j < len(batches); j++ {
		for _, skippedIdx := range batches[j].tasks {
			if j == startBatchIdx && skippedIdx <= skipTaskIdx {
				continue // Already executed or failed
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

			resChan <- toolExecResult{
				index: skippedIdx,
				name:  calls[skippedIdx].Name,
				tr: tools.ToolResult{
					Text:  text,
					Error: resErr,
				},
			}
		}
	}
}

func (e *Orchestrator) executeSerialBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, resChan chan<- toolExecResult) bool {
	taskIdx := batch.tasks[0]
	fc := calls[taskIdx]
	return e.executeSerialTask(ctx, taskIdx, fc, resChan)
}

func (e *Orchestrator) executeParallelBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, resChan chan<- toolExecResult) {
	var wg sync.WaitGroup
	for _, taskIdx := range batch.tasks {
		if err := ctx.Err(); err != nil {
			resChan <- toolExecResult{
				index: taskIdx,
				name:  calls[taskIdx].Name,
				tr: tools.ToolResult{
					Text:  fmt.Sprintf("batch interrupted: %v", err),
					Error: fmt.Errorf("batch interrupted: %w", err),
				},
			}
			continue
		}
		fc := calls[taskIdx]
		e.enqueueParallelTask(ctx, taskIdx, fc, resChan, &wg)
	}
	wg.Wait()
}

func (e *Orchestrator) buildExecutionBatches(calls []*llm.FunctionCall, declinedMap map[int]bool, resChan chan<- toolExecResult) []taskBatch {
	e.mu.RLock()
	reg := e.registry
	e.mu.RUnlock()

	var batches []taskBatch
	var currentParallelBatch []int

	for i, fc := range calls {
		if declinedMap[i] {
			resChan <- toolExecResult{
				index: i,
				name:  fc.Name,
				tr: tools.ToolResult{
					Text:  "User explicitly denied this action.",
					Error: tools.ErrUserDeclined,
				},
			}
			continue
		}

		if reg.IsSerial(fc.Name) {
			// Close current parallel batch if any
			if len(currentParallelBatch) > 0 {
				batches = append(batches, taskBatch{
					isSerial: false,
					tasks:    currentParallelBatch,
				})
				currentParallelBatch = nil
			}
			// Add serial batch
			batches = append(batches, taskBatch{
				isSerial: true,
				tasks:    []int{i},
			})
		} else {
			currentParallelBatch = append(currentParallelBatch, i)
		}
	}

	// Add final parallel batch if any
	if len(currentParallelBatch) > 0 {
		batches = append(batches, taskBatch{
			isSerial: false,
			tasks:    currentParallelBatch,
		})
	}

	return batches
}

func (e *Orchestrator) executeSerialTask(ctx context.Context, i int, fc *llm.FunctionCall, resChan chan<- toolExecResult) bool {
	tr := e.executeTool(ctx, fc)
	resChan <- toolExecResult{index: i, name: fc.Name, tr: tr}

	// If the serial tool failed, timed out, or context was cancelled, we CANNOT safely
	// continue (especially for timeouts/cancellations where the goroutine is orphaned).
	return ctx.Err() == nil && tr.Error == nil
}

func (e *Orchestrator) enqueueParallelTask(ctx context.Context, i int, fc *llm.FunctionCall, resChan chan<- toolExecResult, wg *sync.WaitGroup) {
	wg.Add(1)

	e.mu.RLock()
	pool := e.pool
	e.mu.RUnlock()

	task := func(_ context.Context) {
		defer wg.Done()

		if ctx.Err() != nil {
			resChan <- toolExecResult{
				index: i,
				name:  fc.Name,
				tr:    tools.ToolResult{Text: "Skipped: Context cancelled"},
			}
			return
		}

		tr := e.executeTool(ctx, fc)
		resChan <- toolExecResult{index: i, name: fc.Name, tr: tr}
	}

	if !pool.Submit(task) {
		wg.Done()
		resChan <- toolExecResult{
			index: i,
			name:  fc.Name,
			tr:    tools.ToolResult{Text: "Error: Task submission failed (pool closed or context cancelled)"},
		}
	}
}

func (e *Orchestrator) executeTool(parentCtx context.Context, call *llm.FunctionCall) (result tools.ToolResult) {
	// CRITICAL: This recover block protects the Orchestrator's main resolution loop.
	// It catches panics that occur during synchronous routing, decorator setup,
	// or telemetry wrapping, ensuring the agent does not crash before the tool is even dispatched.
	defer func() {
		if r := recover(); r != nil {
			result = tools.ToolResult{
				Text:  fmt.Sprintf("Tool %q encountered an internal fatal error (panic) and was terminated.", call.Name),
				Error: fmt.Errorf("%w: Panic detected: %v", llm.ErrTerminal, r),
			}
		}
	}()

	tool, err := e.resolver.Resolve(call)
	if err != nil {
		return tools.ToolResult{Text: err.Error(), Error: fmt.Errorf("%w: %v", llm.ErrTerminal, err)}
	}

	result, err = e.runtime.Execute(parentCtx, tool, call)
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
		// Wrap in terminal error to signal orchestrator should stop this turn
		result.Error = fmt.Errorf("%w: %v", llm.ErrTerminal, result.Error)
	}
	return result
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

// withToolTimeout sets the timeout for tools.
func withToolTimeout(timeout time.Duration) executorOption {
	return func(e *Orchestrator) {
		e.toolTimeout = timeout
	}
}


// testExecutionPlanFn is a test hook for injecting a mocked execution plan.
var testExecutionPlanFn func(e *Orchestrator, ctx context.Context, calls []*llm.FunctionCall, resChan chan<- toolExecResult, declinedMap map[int]bool) error
