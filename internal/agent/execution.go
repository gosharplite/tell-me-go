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
	"google.golang.org/genai"
)

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

	if turn >= a.maxToolTurns {
		func() {
			a.sm.TerminalLock()
			defer a.sm.TerminalUnlock()
			fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Error] Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.\033[0m\n",
				time.Now().Format("15:04:05"), a.maxToolTurns)
		}()
		return ErrMaxTurnsReached
	}

	a.logToolCalls(functionCalls, turn)
	responseParts := a.executeToolsConcurrently(ctx, functionCalls)

	a.history.AddContent(&types.Content{
		Role:  "user",
		Parts: responseParts,
	})
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

	fmt.Fprintf(os.Stderr, "%s[%s] %s[Tool Engine (%s%d%s/%d)] Calling: %s%s\n",
		cyan, time.Now().Format("15:04:05"), cyan, reset, turn+1, cyan, a.maxToolTurns, strings.Join(names, ", "), reset)

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

func (a *Agent) executeToolsConcurrently(ctx context.Context, calls []*types.FunctionCall) []*types.Part {
	trs := make([]types.ToolResult, len(calls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, a.maxConcurrentTools)

	for i, fc := range calls {
		if a.isSerialTool(fc.Name) {
			wg.Wait()
			func() {
				defer func() {
					if r := recover(); r != nil {
						trs[i] = types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}
					}
				}()
				trs[i] = a.executeTool(ctx, fc)
			}()
		} else {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, call *types.FunctionCall) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					if r := recover(); r != nil {
						trs[idx] = types.ToolResult{Text: fmt.Sprintf("Error: Panic detected: %v", r)}
					}
				}()
				trs[idx] = a.executeTool(ctx, call)
			}(i, fc)
		}
	}
	wg.Wait()

	var finalParts []*types.Part
	for i, tr := range trs {
		finalParts = append(finalParts, a.processToolResult(calls[i].Name, tr))
		for _, b := range tr.BinaryData {
			finalParts = append(finalParts, &types.Part{
				InlineData: &genai.Blob{
					MIMEType: b.MIMEType,
					Data:     b.Data,
				},
			})
		}
	}

	return finalParts
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
