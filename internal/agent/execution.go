// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
)

type toolExecResult struct {
	index int
	name  string
	tr    types.ToolResult
}

func (a *Agent) handleToolExecution(ctx context.Context, respContent *types.Content, turn int) error {
	var functionCalls []*types.FunctionCall
	for _, part := range respContent.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}

	if len(functionCalls) == 0 {
		return nil
	}

	_, maxToolTurns, _ := a.contextManager.GetLimits()

	if turn >= maxToolTurns {
		func() {
			a.sm.TerminalLock()
			defer a.sm.TerminalUnlock()
			fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Error] Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.\033[0m\n",
				time.Now().Format("15:04:05"), maxToolTurns)
		}()
		return ErrMaxTurnsReached
	}

	a.logToolCalls(functionCalls, turn)

	resChan := make(chan toolExecResult, len(functionCalls))
	var wg sync.WaitGroup

	// Run execution in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.executeToolsConcurrentStream(ctx, functionCalls, resChan)
	}()

	// Collect results as they arrive
	trs := make([]types.ToolResult, len(functionCalls))
	completedCount := 0
	for completedCount < len(functionCalls) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-resChan:
			trs[res.index] = res.tr
			a.renderer.LogToolResult(res.name, res.tr, a.showTools)
			completedCount++
		}
	}
	wg.Wait()

	var responseParts []*types.Part
	for i, tr := range trs {
		responseParts = append(responseParts, a.processToolResult(functionCalls[i].Name, tr))
		for _, b := range tr.BinaryData {
			responseParts = append(responseParts, &types.Part{
				InlineData: &types.Blob{
					MIMEType: b.MIMEType,
					Data:     b.Data,
				},
			})
		}
	}

	if err := a.history.AddContent(&types.Content{
		Role:  "user",
		Parts: responseParts,
	}); err != nil {
		a.reportHistoryError(err)
	}
	return nil
}

func (a *Agent) logToolCalls(calls []*types.FunctionCall, turn int) {
	a.sm.TerminalLock()
	defer a.sm.TerminalUnlock()

	var names []string
	for _, fc := range calls {
		names = append(names, fc.Name)
	}

	cyan := "\033[0;36m"
	reset := "\033[0m"

	_, maxToolTurns, _ := a.contextManager.GetLimits()

	fmt.Fprintf(os.Stderr, "%s[%s] %s[Tool Engine (%s%d%s/%d)] Calling: %s%s\n",
		cyan, time.Now().Format("15:04:05"), cyan, reset, turn+1, cyan, maxToolTurns, strings.Join(names, ", "), reset)

	if a.showTools {
		for _, fc := range calls {
			var argParts []string
			for k, v := range fc.Args {
				valStr := fmt.Sprintf("%v", v)
				if len(valStr) > 60 {
					valStr = valStr[:57] + "..."
				}
				argParts = append(argParts, fmt.Sprintf("%s: %v", k, valStr))
			}
			fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [Tool Action] %s(%s)\033[0m\n",
				time.Now().Format("15:04:05"), fc.Name, strings.Join(argParts, ", "))
		}
	}
}

func (a *Agent) processToolResult(name string, result types.ToolResult) *types.Part {
	return &types.Part{
		FunctionResponse: &types.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

func (a *Agent) executeToolsConcurrentStream(ctx context.Context, calls []*types.FunctionCall, resChan chan<- toolExecResult) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, a.maxConcurrentTools)

	for i, fc := range calls {
		if a.isSerialTool(fc.Name) {
			// Wait for all previous tools to finish before starting serial tool
			wg.Wait()
			func() {
				defer func() {
					if r := recover(); r != nil {
						resChan <- toolExecResult{index: i, name: fc.Name, tr: types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}}
					}
				}()
				tr := a.executeTool(ctx, fc)
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
				tr := a.executeTool(ctx, call)
				resChan <- toolExecResult{index: idx, name: call.Name, tr: tr}
			}(i, fc)
		}
	}
	wg.Wait()
}

func (a *Agent) isSerialTool(name string) bool {
	return a.registry.IsSerial(name)
}

func (a *Agent) executeTool(parentCtx context.Context, call *types.FunctionCall) types.ToolResult {
	// Execute with timeout (exclude interactive/long-running tools)
	var ctx context.Context
	var cancel context.CancelFunc

	if a.registry.IsLongRunning(call.Name) {
		ctx, cancel = context.WithCancel(parentCtx)
	} else {
		ctx, cancel = context.WithTimeout(parentCtx, a.toolTimeout)
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
		result, err := a.registry.Execute(ctx, call.Name, call.Args)
		resChan <- res{tr: result, err: err}
	}()

	select {
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return types.ToolResult{Text: fmt.Sprintf("Error: Tool execution timed out after %v", a.toolTimeout)}
		}
		return types.ToolResult{Text: fmt.Sprintf("Error: %v", ctx.Err())}
	case r := <-resChan:
		if r.err != nil {
			return types.ToolResult{Text: fmt.Sprintf("Error: %v", r.err)}
		}
		return r.tr
	}
}
