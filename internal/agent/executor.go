// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type toolExecResult struct {
	index int
	name  string
	tr    types.ToolResult
}

// ToolExecutor handles the execution of tools, including concurrency and serial locking.
type ToolExecutor struct {
	registry           *tools.Registry
	sm                 *tools.SecurityManager
	renderer           UIRenderer
	maxConcurrentTools int
	toolTimeout        time.Duration
	showTools          bool
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(registry *tools.Registry, sm *tools.SecurityManager, renderer UIRenderer) *ToolExecutor {
	return &ToolExecutor{
		registry:           registry,
		sm:                 sm,
		renderer:           renderer,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		showTools:          true,
	}
}

func (e *ToolExecutor) SetConcurrency(maxConcurrent int, timeout time.Duration) {
	if maxConcurrent > 0 {
		e.maxConcurrentTools = maxConcurrent
	}
	if timeout > 0 {
		e.toolTimeout = timeout
	}
}

func (e *ToolExecutor) SetShowTools(show bool) {
	e.showTools = show
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
		e.renderer.LogSystemMessage(fmt.Sprintf("Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.", maxToolTurns), "error")
		return nil, ErrMaxTurnsReached
	}

	e.renderer.LogToolCall(functionCalls, turn, maxToolTurns, e.showTools)

	resChan := make(chan toolExecResult, len(functionCalls))
	var wg sync.WaitGroup

	// Run execution in background
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
			e.renderer.LogToolResult(res.name, res.tr, e.showTools)
			completedCount++
		}
	}
	wg.Wait()

	var responseParts []*types.Part
	for i, tr := range trs {
		responseParts = append(responseParts, e.processToolResult(functionCalls[i].Name, tr))
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

func (e *ToolExecutor) processToolResult(name string, result types.ToolResult) *types.Part {
	return &types.Part{
		FunctionResponse: &types.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

func (e *ToolExecutor) executeToolsConcurrentStream(ctx context.Context, calls []*types.FunctionCall, resChan chan<- toolExecResult) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxConcurrentTools)

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
			sem <- struct{}{}
			go func(idx int, call *types.FunctionCall) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if r := recover(); r != nil {
						resChan <- toolExecResult{index: idx, name: call.Name, tr: types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}}
					}
				}()
				tr := e.executeTool(ctx, call)
				resChan <- toolExecResult{index: idx, name: call.Name, tr: tr}
			}(i, fc)
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
