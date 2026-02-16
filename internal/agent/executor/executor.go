// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
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
	sm                 domain_security.ISecurityManager
	events             events.EventBus
	maxConcurrentTools int
	toolTimeout        time.Duration
	pool               *concurrency.WorkerPool
	strategy           resultStrategy
	failures           *failureTracker
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, bus events.EventBus) *ToolExecutor {
	e := &ToolExecutor{
		registry:           registry,
		sm:                 sm,
		events:             bus,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		pool:               concurrency.NewWorkerPool(5),
		strategy:           &markdownStrategy{},
		failures:           newFailureTracker(3), // Default threshold of 3
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
	completed := 0
	for completed < len(c.calls) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-c.ch:
			c.trs[res.index] = res.tr
			if c.bus != nil {
				c.bus.Publish(events.ToolResultEvent{Name: res.name, Result: res.tr})
			}
			completed++
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
	e.mu.RUnlock()

	// Orchestrate Execution
	collector := newResultCollector(calls, bus)
	go e.runExecutionPlan(ctx, calls, collector.ch)

	results, err := collector.Wait(ctx)
	if err != nil {
		return nil, err
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

	return e.assembleResponse(calls, results), nil
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

func (e *ToolExecutor) runExecutionPlan(ctx context.Context, calls []*llm.FunctionCall, resChan chan<- toolExecResult) {
	var wg sync.WaitGroup

	for i, fc := range calls {
		e.mu.RLock()
		reg := e.registry
		e.mu.RUnlock()

		if reg.IsSerial(fc.Name) {
			if !e.executeSerialTask(ctx, i, fc, resChan, &wg) {
				// Fill remaining slots in resChan so the collector doesn't hang
				for j := i + 1; j < len(calls); j++ {
					resChan <- toolExecResult{
						index: j,
						name:  calls[j].Name,
						tr:    domaintools.ToolResult{Text: "Skipped: Execution halted due to previous serial tool error, timeout or cancellation."},
					}
				}
				return // Exit the execution plan early
			}
		} else {
			e.enqueueParallelTask(ctx, i, fc, resChan, &wg)
		}
	}
	wg.Wait()
}

func (e *ToolExecutor) executeSerialTask(ctx context.Context, i int, fc *llm.FunctionCall, resChan chan<- toolExecResult, wg *sync.WaitGroup) bool {
	// Wait for all previous tools to finish before starting serial tool
	wg.Wait()
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
		Text:  fmt.Sprintf("Error: Panic detected: %v (in tool %q)", r, toolName),
		Error: fmt.Errorf("%w: Panic detected: %v", llm.ErrTerminal, r),
	}
}

func (e *ToolExecutor) executeTool(parentCtx context.Context, call *llm.FunctionCall) domaintools.ToolResult {
	startTime := time.Now()
	trace := telemetry.TraceFromContext(parentCtx)

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
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName:  call.Name,
			StartTime: startTime,
			Duration:  time.Since(startTime),
			Status:    "error",
			Error:     err.Error(),
		})
		return tr
	}

	// 2. Authorize
	if err := e.authorizeTool(tool, call); err != nil {
		tr := e.errorToToolResult(err)
		trace.RecordToolExecution(telemetry.ToolExecutionTrace{
			ToolName:  call.Name,
			StartTime: startTime,
			Duration:  time.Since(startTime),
			Status:    "error",
			Error:     err.Error(),
		})
		return tr
	}

	// 3. Execute (with recovery/timeout)
	result, err := e.runWithTimeout(parentCtx, tool, call.Args)

	status := "success"
	var errStr string
	if err != nil || result.Error != nil {
		status = "error"
		if err != nil {
			errStr = err.Error()
		} else {
			errStr = result.Error.Error()
		}
		e.failures.recordFailure(call.Name)
	} else {
		e.failures.recordSuccess(call.Name)
	}

	trace.RecordToolExecution(telemetry.ToolExecutionTrace{
		ToolName:  call.Name,
		StartTime: startTime,
		Duration:  time.Since(startTime),
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

		if suggestion := e.suggestTool(call.Name, validTools); suggestion != "" {
			errorMessage += fmt.Sprintf(" Did you mean %q?", suggestion)
		}

		errorMessage += " Please check the spelling or use a different tool from the authorized list."

		return nil, fmt.Errorf("%w: %s", llm.ErrTerminal, errorMessage)
	}

	return tool, nil
}

func (e *ToolExecutor) authorizeTool(tool *domaintools.ToolDeclaration, call *llm.FunctionCall) error {
	e.mu.RLock()
	sm := e.sm
	e.mu.RUnlock()

	if sm != nil && !sm.IsCommandAllowed(call.Name) {
		msg := fmt.Sprintf("Error: Security policy: command %q is not allowed", call.Name)
		return fmt.Errorf("%w: %s", llm.ErrTerminal, msg)
	}

	return nil
}

func (e *ToolExecutor) runWithTimeout(parentCtx context.Context, tool *domaintools.ToolDeclaration, args map[string]interface{}) (domaintools.ToolResult, error) {
	e.mu.RLock()
	reg := e.registry
	toolTimeout := e.toolTimeout
	e.mu.RUnlock()

	// Execute with timeout (exclude interactive/long-running tools)
	var ctx context.Context
	var cancel context.CancelFunc

	if reg.IsLongRunning(tool.Name) {
		ctx, cancel = context.WithCancel(parentCtx)
	} else {
		ctx, cancel = context.WithTimeout(parentCtx, toolTimeout)
	}
	defer cancel()

	type res struct {
		tr  domaintools.ToolResult
		err error
	}
	resChan := make(chan res, 1)
	startTime := time.Now()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resChan <- res{
					tr: e.handlePanic(r, tool.Name),
				}
			}
		}()
		// Tool implementations MUST respect the context (ctx) to prevent goroutine leaks.
		result, err := reg.Execute(ctx, tool.Name, args)
		resChan <- res{tr: result, err: err}
	}()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		msg := fmt.Sprintf("Error: Tool execution failed: %v", err)
		if err == context.DeadlineExceeded {
			msg = fmt.Sprintf("Error: Tool execution timed out after %v", toolTimeout)
		}

		// Spawn watcher for the "Zombie" tool
		go func(toolName string, startTime time.Time) {
			// This goroutine waits indefinitely for the non-compliant tool to return
			<-resChan
			actualDuration := time.Since(startTime)

			e.mu.RLock()
			bus := e.events
			e.mu.RUnlock()
			if bus != nil {
				bus.Publish(events.SystemMessageEvent{
					Message: fmt.Sprintf("Telemetry: Non-compliant tool %q finally finished after %v (exceeded timeout)", toolName, actualDuration),
					Level:   "warn",
				})
			}
		}(tool.Name, startTime)

		return domaintools.ToolResult{
			Text:  msg,
			Error: fmt.Errorf("%w: %s", llm.ErrTransient, msg),
		}, nil
	case r := <-resChan:
		return r.tr, r.err
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

func (e *ToolExecutor) suggestTool(hallucinated string, validTools []string) string {
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
