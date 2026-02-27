// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type toolExecResult struct {
	index int
	name  string
	tr    domaintools.ToolResult
}

// ToolExecutor handles the execution of tools, using a WorkerPool for concurrency.
type ToolExecutor struct {
	mu                 sync.RWMutex
	registry           domaintools.IToolRegistry
	authorizer         ToolAuthorizer
	events             events.EventBus
	maxConcurrentTools int
	toolTimeout        time.Duration
	longRunningTimeout time.Duration
	zombieTimeout      time.Duration
	pool               *concurrency.WorkerPool
	strategy           resultStrategy
	failures           *failureTracker
}

// ExecutorOption allows configuring the ToolExecutor.
type ExecutorOption func(*ToolExecutor)

// WithLongRunningTimeout sets the timeout for long-running tools.
func WithLongRunningTimeout(timeout time.Duration) ExecutorOption {
	return func(e *ToolExecutor) {
		e.longRunningTimeout = timeout
	}
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, bus events.EventBus, opts ...ExecutorOption) *ToolExecutor {
	e := &ToolExecutor{
		registry:           registry,
		authorizer:         newSecurityAuthorizer(sm, registry),
		events:             bus,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		longRunningTimeout: 5 * time.Minute,
		zombieTimeout:      5 * time.Minute,
		pool:               concurrency.NewWorkerPool(5),
		strategy:           &markdownStrategy{},
		failures:           newFailureTracker(3), // Default threshold of 3
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
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

func (f *failureTracker) isOpen(toolName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.failures[toolName] >= f.threshold
}

// setStrategy sets the result formatting strategy.
func (e *ToolExecutor) setStrategy(s resultStrategy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.strategy = s
}

func (e *ToolExecutor) SetConcurrency(maxConcurrent int, timeout time.Duration) {
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
func (e *ToolExecutor) Shutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pool != nil {
		e.pool.Shutdown()
	}
}

type resultCollector struct {
	calls []*llm.FunctionCall
	bus   events.EventBus
	trs   []domaintools.ToolResult
	ch    chan toolExecResult
}

func newResultCollector(calls []*llm.FunctionCall, bus events.EventBus) *resultCollector {
	return &resultCollector{
		calls: calls,
		bus:   bus,
		trs:   make([]domaintools.ToolResult, len(calls)),
		ch:    make(chan toolExecResult, len(calls)),
	}
}

func (c *resultCollector) Wait(ctx context.Context) ([]domaintools.ToolResult, error) {
	completedCount := 0
	isCompleted := make([]bool, len(c.calls))
	for completedCount < len(c.calls) {
		select {
		case <-ctx.Done():
			for i := range c.trs {
				if !isCompleted[i] {
					c.trs[i] = domaintools.ToolResult{Text: "Execution was interrupted or cancelled by the user."}
				}
			}
			return c.trs, ctx.Err()
		case res := <-c.ch:
			if !isCompleted[res.index] {
				c.trs[res.index] = res.tr
				isCompleted[res.index] = true
				if c.bus != nil {
					c.bus.Publish(events.ToolResultEvent{Name: res.name, Result: res.tr})
				}
				completedCount++
			}
		}
	}
	return c.trs, nil
}

// Execute handles the execution of function calls from the model response.
func (e *ToolExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	calls := e.extractFunctionCalls(respContent)
	if len(calls) == 0 {
		return nil, nil
	}

	if turn >= maxToolTurns {
		e.publishLimitError(maxToolTurns)
		return nil, llm.ErrMaxTurnsReached
	}

	e.publishCallEvent(calls, turn, maxToolTurns)

	e.mu.RLock()
	bus := e.events
	auth := e.authorizer
	e.mu.RUnlock()

	declinedMap := auth.RequestBatchConsent(ctx, calls)

	// Orchestrate Execution
	collector := newResultCollector(calls, bus)
	startTime := time.Now()
	go e.runExecutionPlan(ctx, calls, collector.ch, declinedMap)

	results, err := collector.Wait(ctx)
	duration := time.Since(startTime)

	if err != nil {
		slog.Error("Tool execution turn failed or was cancelled",
			slog.Int("turn", turn),
			slog.String("error", err.Error()),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
	} else {
		slog.Info("Tool execution turn completed",
			slog.Int("turn", turn),
			slog.Int("tool_calls", len(calls)),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
	}

	// Notify about circuit breaker events
	for _, tr := range results {
		if errors.Is(tr.Error, domaintools.ErrToolCircuitOpen) {
			e.mu.RLock()
			bus := e.events
			e.mu.RUnlock()
			if bus != nil {
				bus.Publish(events.SystemMessageEvent{
					Message: tr.Text,
					Level:   "warn",
				})
			}
		}
	}

	return e.assembleResponse(calls, results), err
}

func (e *ToolExecutor) extractFunctionCalls(respContent *llm.Content) []*llm.FunctionCall {
	var functionCalls []*llm.FunctionCall
	for _, part := range respContent.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}
	return functionCalls
}

func (e *ToolExecutor) publishLimitError(maxToolTurns int) {
	e.mu.RLock()
	bus := e.events
	e.mu.RUnlock()
	if bus != nil {
		bus.Publish(events.SystemMessageEvent{
			Message: fmt.Sprintf("Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.", maxToolTurns),
			Level:   "error",
		})
	}
}

func (e *ToolExecutor) publishCallEvent(calls []*llm.FunctionCall, turn int, maxToolTurns int) {
	e.mu.RLock()
	bus := e.events
	e.mu.RUnlock()
	if bus != nil {
		bus.Publish(events.ToolCallEvent{
			Calls:    calls,
			Turn:     turn,
			MaxTurns: maxToolTurns,
		})
	}
}

func (e *ToolExecutor) assembleResponse(calls []*llm.FunctionCall, results []domaintools.ToolResult) *llm.Content {
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

func (e *ToolExecutor) runExecutionPlan(ctx context.Context, calls []*llm.FunctionCall, resChan chan<- toolExecResult, declinedMap map[int]bool) {
	batches := e.buildExecutionBatches(calls, declinedMap, resChan)

	for batchIdx, batch := range batches {
		if err := ctx.Err(); err != nil {
			slog.Warn("Execution plan interrupted", slog.String("reason", "context cancelled"), slog.Int("batch_idx", batchIdx))
			e.failRemainingTasks(batches, batchIdx, -1, calls, resChan, err, "batch interrupted")
			return
		}

		batchStart := time.Now()
		if batch.isSerial {
			if !e.executeSerialBatch(ctx, batch, calls, resChan) {
				slog.Warn("Serial batch failed or interrupted, halting execution plan",
					slog.Int("batch_idx", batchIdx),
					slog.String("tool_name", calls[batch.tasks[0]].Name))
				e.failRemainingTasks(batches, batchIdx, batch.tasks[0], calls, resChan, nil, "Skipped: Execution halted due to previous serial tool error, timeout or cancellation.")
				return // Exit the execution plan early
			}
		} else {
			e.executeParallelBatch(ctx, batch, calls, resChan)
		}

		slog.Debug("Batch execution completed",
			slog.Int("batch_idx", batchIdx),
			slog.Bool("is_serial", batch.isSerial),
			slog.Int("task_count", len(batch.tasks)),
			slog.Int64("duration_ms", time.Since(batchStart).Milliseconds()))
	}
}

func (e *ToolExecutor) failRemainingTasks(batches []taskBatch, startBatchIdx int, skipTaskIdx int, calls []*llm.FunctionCall, resChan chan<- toolExecResult, err error, reason string) {
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
				tr: domaintools.ToolResult{
					Text:  text,
					Error: resErr,
				},
			}
		}
	}
}

func (e *ToolExecutor) executeSerialBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, resChan chan<- toolExecResult) bool {
	taskIdx := batch.tasks[0]
	fc := calls[taskIdx]
	return e.executeSerialTask(ctx, taskIdx, fc, resChan)
}

func (e *ToolExecutor) executeParallelBatch(ctx context.Context, batch taskBatch, calls []*llm.FunctionCall, resChan chan<- toolExecResult) {
	var wg sync.WaitGroup
	for _, taskIdx := range batch.tasks {
		if err := ctx.Err(); err != nil {
			resChan <- toolExecResult{
				index: taskIdx,
				name:  calls[taskIdx].Name,
				tr: domaintools.ToolResult{
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

func (e *ToolExecutor) buildExecutionBatches(calls []*llm.FunctionCall, declinedMap map[int]bool, resChan chan<- toolExecResult) []taskBatch {
	e.mu.RLock()
	reg := e.registry
	e.mu.RUnlock()

	var parallelTasks []int
	var serialBatches []taskBatch

	for i, fc := range calls {
		if declinedMap[i] {
			resChan <- toolExecResult{
				index: i,
				name:  fc.Name,
				tr: domaintools.ToolResult{
					Text:  "User explicitly denied this action.",
					Error: domaintools.ErrUserDeclined,
				},
			}
			continue
		}

		if reg.IsSerial(fc.Name) {
			serialBatches = append(serialBatches, taskBatch{
				isSerial: true,
				tasks:    []int{i},
			})
		} else {
			parallelTasks = append(parallelTasks, i)
		}
	}

	var batches []taskBatch
	if len(parallelTasks) > 0 {
		batches = append(batches, taskBatch{
			isSerial: false,
			tasks:    parallelTasks,
		})
	}
	batches = append(batches, serialBatches...)

	return batches
}

func (e *ToolExecutor) executeSerialTask(ctx context.Context, i int, fc *llm.FunctionCall, resChan chan<- toolExecResult) bool {
	var tr domaintools.ToolResult
	func() {
		defer func() {
			if r := recover(); r != nil {
				tr = e.handlePanic(r, fc.Name)
			}
		}()
		tr = e.executeTool(ctx, fc)
	}()
	resChan <- toolExecResult{index: i, name: fc.Name, tr: tr}

	// If the serial tool failed, timed out, or context was cancelled, we CANNOT safely
	// continue (especially for timeouts/cancellations where the goroutine is orphaned).
	return ctx.Err() == nil && tr.Error == nil
}

func (e *ToolExecutor) enqueueParallelTask(ctx context.Context, i int, fc *llm.FunctionCall, resChan chan<- toolExecResult, wg *sync.WaitGroup) {
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
				tr:    domaintools.ToolResult{Text: "Skipped: Context cancelled"},
			}
			return
		}

		defer func() {
			if r := recover(); r != nil {
				resChan <- toolExecResult{
					index: i,
					name:  fc.Name,
					tr:    e.handlePanic(r, fc.Name),
				}
			}
		}()
		tr := e.executeTool(ctx, fc)
		resChan <- toolExecResult{index: i, name: fc.Name, tr: tr}
	}

	if !pool.Submit(task) {
		wg.Done()
		resChan <- toolExecResult{
			index: i,
			name:  fc.Name,
			tr:    domaintools.ToolResult{Text: "Error: Task submission failed (pool closed or context cancelled)"},
		}
	}
}

func (e *ToolExecutor) handlePanic(r interface{}, toolName string) domaintools.ToolResult {
	stack := debug.Stack()

	e.mu.RLock()
	bus := e.events
	e.mu.RUnlock()

	if bus != nil {
		msg := fmt.Sprintf("CRITICAL: Panic in tool executor while running %q: %v\n%s", toolName, r, string(stack))
		bus.Publish(events.SystemMessageEvent{
			Message: msg,
			Level:   "error",
		})
	}

	return domaintools.ToolResult{
		Text:  fmt.Sprintf("Tool %q encountered an internal fatal error (panic) and was terminated.", toolName),
		Error: fmt.Errorf("%w: Panic detected: %v", llm.ErrTerminal, r),
	}
}

func (e *ToolExecutor) executeTool(parentCtx context.Context, call *llm.FunctionCall) domaintools.ToolResult {
	ctx, span := otel.Tracer("agent").Start(parentCtx, "tool.execute."+call.Name)
	span.SetAttributes(attribute.String("tool.name", call.Name))
	defer span.End()

	startTime := time.Now()
	trace := telemetry.TraceFromContext(ctx)

	// Check Circuit Breaker
	if e.failures.isOpen(call.Name) {
		tr := domaintools.ToolResult{
			Text:  fmt.Sprintf("Error: Tool %q is temporarily disabled due to multiple consecutive failures.", call.Name),
			Error: domaintools.ErrToolCircuitOpen,
		}
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName:  call.Name,
			StartTime: startTime,
			Duration:  time.Since(startTime),
			Status:    "circuit_open",
			Error:     tr.Text,
		})
		return tr
	}

	// 1. Resolve
	tool, err := e.resolveTool(call)
	if err != nil {
		tr := e.errorToToolResult(err)
		return e.finalizeToolExecution(call.Name, tr, err, startTime, trace)
	}

	// 2. Authorize
	e.mu.RLock()
	auth := e.authorizer
	e.mu.RUnlock()
	if err := auth.AuthorizeTool(tool, call); err != nil {
		tr := e.errorToToolResult(err)
		return e.finalizeToolExecution(call.Name, tr, err, startTime, trace)
	}

	// 4. Execute (with recovery/timeout)
	result, err := e.runWithTimeout(ctx, tool, call.Args)

	// 5. Finalize
	return e.finalizeToolExecution(call.Name, result, err, startTime, trace)
}

func classifyToolError(err error, resultErr error) (string, string) {
	if errors.Is(err, domaintools.ErrUserDeclined) || (resultErr != nil && errors.Is(resultErr, domaintools.ErrUserDeclined)) {
		return "user_declined", "The user explicitly denied this action. Do not attempt this exact action again. Ask the user for clarification or propose an alternative approach."
	}
	if errors.Is(err, domaintools.ErrSecurityPolicy) || (resultErr != nil && errors.Is(resultErr, domaintools.ErrSecurityPolicy)) {
		return "security_blocked", "Action blocked by the system sandbox security policy. You are not authorized to perform this operation."
	}
	if err != nil || resultErr != nil {
		return "error", ""
	}
	return "success", ""
}

func (e *ToolExecutor) finalizeToolExecution(callName string, result domaintools.ToolResult, err error, startTime time.Time, trace *telemetry.TurnTrace) domaintools.ToolResult {
	status, msg := classifyToolError(err, result.Error)
	duration := time.Since(startTime)

	// [OPERATIONAL READINESS] Add structured logging for execution visibility
	e.mu.RLock()
	isSerial := e.registry.IsSerial(callName)
	isLongRunning := e.registry.IsLongRunning(callName)
	e.mu.RUnlock()

	var errStr string
	if err != nil {
		errStr = err.Error()
	} else if result.Error != nil {
		errStr = result.Error.Error()
	}

	logAttrs := []any{
		slog.String("tool_name", callName),
		slog.Bool("is_serial", isSerial),
		slog.Bool("is_long_running", isLongRunning),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.String("status", status),
	}
	if errStr != "" {
		logAttrs = append(logAttrs, slog.String("error_reason", errStr))
	}

	// [Metrics] TODO: tool_execution_duration_seconds tagged with tool_name and status

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(result.Error, context.DeadlineExceeded) {
		slog.Warn("Tool execution timed out", logAttrs...)
	} else if status == "error" {
		slog.Error("Tool execution failed", logAttrs...)
	} else {
		slog.Info("Tool execution completed", logAttrs...)
	}

	if status == "user_declined" || status == "security_blocked" {
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName:  callName,
			StartTime: startTime,
			Duration:  duration,
			Status:    status,
		})
		return domaintools.ToolResult{Text: msg, Error: nil}
	}

	if status == "error" {
		e.failures.recordFailure(callName)
	} else {
		e.failures.recordSuccess(callName)
	}

	trace.RecordToolExecution(telemetry.ToolExecutionTrace{
		ToolName:  callName,
		StartTime: startTime,
		Duration:  duration,
		Status:    status,
		Error:     errStr,
	})

	if err != nil {
		msg := fmt.Sprintf("Error: %v", err)
		return domaintools.ToolResult{
			Text:  msg,
			Error: fmt.Errorf("%w: %s", llm.ErrTerminal, msg),
		}
	}

	return result
}

func (e *ToolExecutor) resolveTool(call *llm.FunctionCall) (*domaintools.ToolDeclaration, error) {
	e.mu.RLock()
	reg := e.registry
	e.mu.RUnlock()

	return resolveTool(reg, call)
}

func resolveTool(reg domaintools.IToolRegistry, call *llm.FunctionCall) (*domaintools.ToolDeclaration, error) {
	var tool *domaintools.ToolDeclaration
	var validTools []string
	for _, decl := range reg.GetDeclarations() {
		validTools = append(validTools, decl.Name)
		if decl.Name == call.Name {
			tool = decl
		}
	}

	if tool == nil {
		sort.Strings(validTools)
		errorMessage := fmt.Sprintf(
			"Error: Tool %q is not defined. Available tools are: [%s].",
			call.Name, strings.Join(validTools, ", "),
		)

		if suggestion := suggestTool(call.Name, validTools); suggestion != "" {
			errorMessage += fmt.Sprintf(" Did you mean %q?", suggestion)
		}

		errorMessage += " Please check the spelling or use a different tool from the authorized list."

		return nil, fmt.Errorf("%w: %s", llm.ErrTerminal, errorMessage)
	}

	return tool, nil
}

func (e *ToolExecutor) runWithTimeout(parentCtx context.Context, tool *domaintools.ToolDeclaration, args map[string]interface{}) (domaintools.ToolResult, error) {
	e.mu.RLock()
	reg := e.registry
	toolTimeout := e.toolTimeout
	longRunningTimeout := e.longRunningTimeout
	e.mu.RUnlock()

	var ctx context.Context
	var cancel context.CancelFunc

	activeTimeout := toolTimeout
	if reg.IsLongRunning(tool.Name) {
		activeTimeout = longRunningTimeout
	}
	ctx, cancel = context.WithTimeout(parentCtx, activeTimeout)
	defer cancel()

	// Buffered channel prevents goroutine leak if the tool finishes after timeout
	outCh := make(chan toolOutput, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				outCh <- toolOutput{
					result: e.handlePanic(r, tool.Name),
					err:    nil,
				}
			}
		}()
		res, execErr := reg.Execute(ctx, tool.Name, args)
		outCh <- toolOutput{result: res, err: execErr}
	}()

	select {
	case <-ctx.Done():
		// The context expired (timeout or parent cancellation) before the tool finished
		errCtx := ctx.Err()
		msg := fmt.Sprintf("Error: Tool execution failed: %v", errCtx)
		errorWrapMsg := "tool execution failed"
		if errCtx == context.DeadlineExceeded {
			msg = fmt.Sprintf("Error: Tool execution timed out after %v", activeTimeout)
			errorWrapMsg = "tool execution timed out"
		}

		// SCALABLE (GOOD): Implementing a telemetry watchdog for abandoned goroutines
		go e.monitorZombieTool(parentCtx, tool.Name, time.Now(), outCh)

		return domaintools.ToolResult{
			Text:  msg,
			Error: fmt.Errorf("%w: %s: %w", llm.ErrTransient, errorWrapMsg, errCtx),
		}, nil

	case out := <-outCh:
		// Tool finished successfully within the deadline
		return out.result, out.err
	}
}

func (e *ToolExecutor) errorToToolResult(err error) domaintools.ToolResult {
	msg := err.Error()
	// Since we no longer use AgentError in subpackages, we don't need this check here
	// unless we want to keep support for it if it comes from elsewhere.
	// But to break cycle we can't use agent.AgentError.
	return domaintools.ToolResult{
		Text:  msg,
		Error: err,
	}
}

func suggestTool(hallucinated string, validTools []string) string {
	closest := ""
	hallucinatedLower := strings.ToLower(hallucinated)

	// Start with a threshold based on length, max 3.
	// For very short names (<=3), we want distance 1.
	// For medium names, distance 2.
	// For long names, distance 3.
	minDist := 1
	if len(hallucinated) > 6 {
		minDist = 3
	} else if len(hallucinated) > 3 {
		minDist = 2
	}

	for _, tool := range validTools {
		toolLower := strings.ToLower(tool)
		dist := stringsutil.LevenshteinDistance(hallucinatedLower, toolLower)
		if dist <= minDist {
			minDist = dist
			closest = tool
		}
	}
	return closest
}

func (e *ToolExecutor) monitorZombieTool(ctx context.Context, name string, start time.Time, outCh <-chan toolOutput) {
	e.mu.RLock()
	zombieTimeout := e.zombieTimeout
	e.mu.RUnlock()

	timer := time.NewTimer(zombieTimeout)
	defer timer.Stop()

	select {
	case <-outCh:
		// Tool eventually finished, log the extreme latency
		telemetry.RecordLateCompletion(name, time.Since(start))
	case <-timer.C:
		// Tool is permanently deadlocked
		telemetry.LogCritical("CRITICAL: Tool goroutine permanently leaked", name)
	case <-ctx.Done():
		// Application/Session shutting down; safe to abandon due to buffered outCh
		return
	}
}

type toolOutput struct {
	result domaintools.ToolResult
	err    error
}
