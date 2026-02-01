// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type toolExecResult struct {
	index int
	name  string
	tr    types.ToolResult
}

// ToolExecutor handles the execution of tools, using a WorkerPool for concurrency.
type ToolExecutor struct {
	registry           *registry.Registry
	sm                 *tools.SecurityManager
	events             events.EventBus
	maxConcurrentTools int
	toolTimeout        time.Duration
	pool               *WorkerPool
	strategy           ResultStrategy
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(registry *registry.Registry, sm *tools.SecurityManager, bus events.EventBus) *ToolExecutor {
	return &ToolExecutor{
		registry:           registry,
		sm:                 sm,
		events:             bus,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		pool:               NewWorkerPool(5),
		strategy:           &MarkdownStrategy{},
	}
}

// SetStrategy sets the result formatting strategy.
func (e *ToolExecutor) SetStrategy(s ResultStrategy) {
	e.strategy = s
}

func (e *ToolExecutor) SetConcurrency(maxConcurrent int, timeout time.Duration) {
	if maxConcurrent > 0 && maxConcurrent != e.maxConcurrentTools {
		e.maxConcurrentTools = maxConcurrent
		if e.pool != nil {
			e.pool.Shutdown()
		}
		e.pool = NewWorkerPool(maxConcurrent)
	}
	if timeout > 0 {
		e.toolTimeout = timeout
	}
}

// Execute handles the execution of function calls from the model response.
func (e *ToolExecutor) Execute(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error) {
	var functionCalls []*types.FunctionCall
	for _, part := range respContent.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}

	if len(functionCalls) == 0 {
		return nil, nil
	}

	if turn >= maxToolTurns {
		if e.events != nil {
			e.events.Publish(events.SystemMessageEvent{
				Message: fmt.Sprintf("Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.", maxToolTurns),
				Level:   "error",
			})
		}
		return nil, types.ErrMaxTurnsReached
	}

	if e.events != nil {
		e.events.Publish(events.ToolCallEvent{
			Calls:    functionCalls,
			Turn:     turn,
			MaxTurns: maxToolTurns,
		})
	}

	resChan := make(chan toolExecResult, len(functionCalls))
	var wg sync.WaitGroup

	// Run execution orchestration in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.executeToolsConcurrentStream(ctx, functionCalls, resChan)
	}()

	// Collect results as they arrive
	trs := make([]types.ToolResult, len(functionCalls))
	completedCount := 0
	for completedCount < len(functionCalls) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-resChan:
			trs[res.index] = res.tr
			if e.events != nil {
				e.events.Publish(events.ToolResultEvent{
					Name:   res.name,
					Result: res.tr,
				})
			}
			completedCount++
		}
	}
	wg.Wait()

	var responseParts []*types.Part
	for i, tr := range trs {
		responseParts = append(responseParts, e.strategy.Format(functionCalls[i].Name, tr))
		for _, b := range tr.BinaryData {
			responseParts = append(responseParts, &types.Part{
				InlineData: &types.Blob{
					MIMEType: b.MIMEType,
					Data:     b.Data,
				},
			})
		}
	}

	return &types.Content{
		Role:  "user",
		Parts: responseParts,
	}, nil
}

func (e *ToolExecutor) executeToolsConcurrentStream(ctx context.Context, calls []*types.FunctionCall, resChan chan<- toolExecResult) {
	var wg sync.WaitGroup

	for i, fc := range calls {
		if e.registry.IsSerial(fc.Name) {
			// Wait for all previous tools to finish before starting serial tool
			wg.Wait()
			func() {
				defer func() {
					if r := recover(); r != nil {
						resChan <- toolExecResult{index: i, name: fc.Name, tr: types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}}
					}
				}()
				tr := e.executeTool(ctx, fc)
				resChan <- toolExecResult{index: i, name: fc.Name, tr: tr}
			}()
		} else {
			wg.Add(1)
			idx, call := i, fc // captured for closure
			e.pool.Submit(func(_ context.Context) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						resChan <- toolExecResult{index: idx, name: call.Name, tr: types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}}
					}
				}()
				tr := e.executeTool(ctx, call)
				resChan <- toolExecResult{index: idx, name: call.Name, tr: tr}
			})
		}
	}
	wg.Wait()
}

func (e *ToolExecutor) executeTool(parentCtx context.Context, call *types.FunctionCall) types.ToolResult {
	// Execute with timeout (exclude interactive/long-running tools)
	var ctx context.Context
	var cancel context.CancelFunc

	if e.registry.IsLongRunning(call.Name) {
		ctx, cancel = context.WithCancel(parentCtx)
	} else {
		ctx, cancel = context.WithTimeout(parentCtx, e.toolTimeout)
	}
	defer cancel()

	type res struct {
		tr  types.ToolResult
		err error
	}
	resChan := make(chan res, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resChan <- res{tr: types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}}
			}
		}()
		// Tool implementations MUST respect the context (ctx) to prevent goroutine leaks.
		result, err := e.registry.Execute(ctx, call.Name, call.Args)
		resChan <- res{tr: result, err: err}
	}()

	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return types.ToolResult{Text: fmt.Sprintf("Error: Tool execution timed out after %v", e.toolTimeout)}
		}
		return types.ToolResult{Text: fmt.Sprintf("Error: %v", ctx.Err())}
	case r := <-resChan:
		if r.err != nil {
			return types.ToolResult{Text: fmt.Sprintf("Error: %v", r.err)}
		}
		return r.tr
	}
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

// Submit adds a task to the pool.
func (p *WorkerPool) Submit(task func(ctx context.Context)) {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return
	}
	p.mu.RUnlock()

	select {
	case p.tasks <- task:
	case <-p.closing:
	case <-p.ctx.Done():
	}
}

// Shutdown stops all workers and waits for them to finish.
func (p *WorkerPool) Shutdown() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()

		close(p.closing)
		close(p.tasks)
		p.wg.Wait()
		p.cancel()
	})
}
