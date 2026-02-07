// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenerrors"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	sm                 security.ISecurityManager
	events             events.EventBus
	maxConcurrentTools int
	toolTimeout        time.Duration
	pool               *WorkerPool
	strategy           ResultStrategy
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(registry domaintools.IToolRegistry, sm security.ISecurityManager, bus events.EventBus) *ToolExecutor {
	e := &ToolExecutor{
		registry:           registry,
		sm:                 sm,
		events:             bus,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		pool:               NewWorkerPool(5),
		strategy:           &MarkdownStrategy{},
	}

	if bus != nil {
		// Event subscriptions could be added here if needed in the future.
	}

	return e
}

// SetStrategy sets the result formatting strategy.
func (e *ToolExecutor) SetStrategy(s ResultStrategy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.strategy = s
}

func (e *ToolExecutor) SetConcurrency(maxConcurrent int, timeout time.Duration) {
	var oldPool *WorkerPool
	e.mu.Lock()

	if maxConcurrent > 0 && maxConcurrent != e.maxConcurrentTools {
		e.maxConcurrentTools = maxConcurrent
		if e.pool != nil {
			oldPool = e.pool
		}
		e.pool = NewWorkerPool(maxConcurrent)
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
		responseParts = append(responseParts, strategy.Format(calls[i].Name, tr))
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
		pool := e.pool
		e.mu.RUnlock()

		if reg.IsSerial(fc.Name) {
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
			if ctx.Err() != nil || tr.Error != nil {
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
			wg.Add(1)
			idx, call := i, fc // captured for closure

			task := func(_ context.Context) {
				defer wg.Done()

				if ctx.Err() != nil {
					resChan <- toolExecResult{
						index: idx,
						name:  call.Name,
						tr:    domaintools.ToolResult{Text: "Skipped: Context cancelled"},
					}
					return
				}

				defer func() {
					if r := recover(); r != nil {
						resChan <- toolExecResult{
							index: idx,
							name:  call.Name,
							tr:    e.handlePanic(r, call.Name),
						}
					}
				}()
				tr := e.executeTool(ctx, call)
				resChan <- toolExecResult{index: idx, name: call.Name, tr: tr}
			}

			if !pool.Submit(task) {
				wg.Done()
				resChan <- toolExecResult{
					index: idx,
					name:  call.Name,
					tr:    domaintools.ToolResult{Text: "Error: Task submission failed (pool closed or context cancelled)"},
				}
			}
		}
	}
	wg.Wait()
}

func (e *ToolExecutor) handlePanic(r interface{}, toolName string) domaintools.ToolResult {
	stack := debug.Stack()
	err := fmt.Errorf("panic: %v", r)

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
		Error: agenerrors.NewAgentError(agenerrors.ErrFatal, fmt.Sprintf("Panic detected: %v", r), err),
	}
}

func (e *ToolExecutor) executeTool(parentCtx context.Context, call *llm.FunctionCall) domaintools.ToolResult {
	// 1. Resolve
	tool, err := e.resolveTool(call)
	if err != nil {
		return e.errorToToolResult(err)
	}

	// 2. Authorize
	if err := e.authorizeTool(tool, call); err != nil {
		return e.errorToToolResult(err)
	}

	// 3. Execute (with recovery/timeout)
	result, err := e.runWithTimeout(parentCtx, tool, call.Args)
	if err != nil {
		msg := fmt.Sprintf("Error: %v", err)
		return domaintools.ToolResult{
			Text:  msg,
			Error: agenerrors.NewAgentError(agenerrors.ErrLogic, msg, err),
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

		return nil, agenerrors.NewAgentError(agenerrors.ErrLogic, errorMessage, fmt.Errorf("unexpected tool call: %s", call.Name))
	}

	return tool, nil
}

func (e *ToolExecutor) authorizeTool(tool *domaintools.ToolDeclaration, call *llm.FunctionCall) error {
	e.mu.RLock()
	sm := e.sm
	e.mu.RUnlock()

	if sm != nil && !sm.IsCommandAllowed(call.Name) {
		msg := fmt.Sprintf("Error: Security policy: command %q is not allowed", call.Name)
		return agenerrors.NewAgentError(agenerrors.ErrLogic, msg, fmt.Errorf("security policy: command %q is not allowed", call.Name))
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
		return domaintools.ToolResult{
			Text:  msg,
			Error: agenerrors.NewAgentError(agenerrors.ErrTransient, msg, err),
		}, nil
	case r := <-resChan:
		return r.tr, r.err
	}
}

func (e *ToolExecutor) errorToToolResult(err error) domaintools.ToolResult {
	msg := err.Error()
	if ae, ok := err.(*agenerrors.AgentError); ok {
		msg = ae.Message
	}
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
		dist := levenshteinDistance(hallucinatedLower, toolLower)
		if dist <= minDist {
			minDist = dist
			closest = tool
		}
	}
	return closest
}

func levenshteinDistance(s, t string) int {
	s1, s2 := []rune(s), []rune(t)
	m, n := len(s1), len(s2)

	if m < n {
		s1, s2 = s2, s1
		m, n = n, m
	}

	prev := make([]int, n+1)
	for j := 0; j <= n; j++ {
		prev[j] = j
	}

	curr := make([]int, n+1)
	for i := 1; i <= m; i++ {
		curr[0] = i
		for j := 1; j <= n; j++ {
			substitutionCost := 0
			if s1[i-1] != s2[j-1] {
				substitutionCost = 1
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+substitutionCost)
		}
		copy(prev, curr)
	}
	return prev[n]
}

// WorkerPool manages a fixed number of workers to execute tasks concurrently.
type WorkerPool struct {
	maxWorkers int
	tasks      chan func(ctx context.Context)
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	closing    chan struct{}
	mu         sync.RWMutex
	closed     bool
	once       sync.Once
}

// NewWorkerPool creates and starts a new worker pool.
func NewWorkerPool(maxWorkers int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &WorkerPool{
		maxWorkers: maxWorkers,
		tasks:      make(chan func(ctx context.Context), maxWorkers*2),
		ctx:        ctx,
		cancel:     cancel,
		closing:    make(chan struct{}),
	}
	p.start()
	return p
}

func (p *WorkerPool) start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for {
				select {
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					task(p.ctx)
				case <-p.ctx.Done():
					return
				}
			}
		}()
	}
}

// Submit adds a task to the pool. Returns true if the task was successfully queued.
func (p *WorkerPool) Submit(task func(ctx context.Context)) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false
	}

	select {
	case p.tasks <- task:
		return true
	case <-p.closing:
		return false
	case <-p.ctx.Done():
		return false
	}
}

// Shutdown stops all workers and waits for them to finish.
func (p *WorkerPool) Shutdown() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.closing)
		close(p.tasks)
		p.mu.Unlock()

		p.wg.Wait()
		p.cancel()
	})
}
