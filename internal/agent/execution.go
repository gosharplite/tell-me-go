// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/api"
	"google.golang.org/genai"
)

func (a *Agent) handleToolExecution(respContent *api.Content, turn int) error {
	var functionCalls []*api.FunctionCall
	for _, part := range respContent.Parts {
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, part.FunctionCall)
		}
	}

	if len(functionCalls) == 0 {
		return nil
	}

	if turn >= a.maxToolTurns {
		a.sm.TerminalLock()
		fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Error] Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.\033[0m\n",
			time.Now().Format("15:04:05"), a.maxToolTurns)
		a.sm.TerminalUnlock()
		return ErrMaxTurnsReached
	}

	a.logToolCalls(functionCalls, turn)
	responseParts := a.executeToolsConcurrently(functionCalls)

	a.history.AddContent(&api.Content{
		Role:  "user",
		Parts: responseParts,
	})
	return nil
}

func (a *Agent) logToolCalls(calls []*api.FunctionCall, turn int) {
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

func (a *Agent) executeToolsConcurrently(calls []*api.FunctionCall) []*api.Part {
	results := make([]*api.Part, len(calls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, a.maxConcurrentTools)

	for i, fc := range calls {
		if a.isSerialTool(fc.Name) {
			// Serialization Logic:
			// To ensure strict execution order as perceived by the model (e.g., Task ID assignment),
			// we wait for all previously dispatched parallel tools to finish before executing
			// the serial tool.
			wg.Wait()
			results[i] = a.processToolResult(fc.Name, a.executeTool(fc))
		} else {
			// Parallel Execution:
			wg.Add(1)
			go func(idx int, call *api.FunctionCall) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				results[idx] = a.processToolResult(call.Name, a.executeTool(call))
			}(i, fc)
		}
	}
	wg.Wait()

	// Post-process for injections
	return a.injectBinaryData(results)
}

func (a *Agent) isSerialTool(name string) bool {
	return a.registry.IsSerial(name)
}

func (a *Agent) executeTool(call *api.FunctionCall) string {
	// Execute with timeout (exclude interactive/long-running tools)
	var ctx context.Context
	var cancel context.CancelFunc

	if a.registry.IsLongRunning(call.Name) {
		ctx = context.Background()
		cancel = func() {}
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), a.toolTimeout)
	}
	defer cancel()

	resChan := make(chan string, 1)
	go func() {
		result, err := a.registry.Execute(ctx, call.Name, call.Args)
		if err != nil {
			resChan <- fmt.Sprintf("Error: %v", err)
		} else {
			resChan <- result
		}
	}()

	select {
	case <-ctx.Done():
		return fmt.Sprintf("Error: Tool execution timed out after %v", a.toolTimeout)
	case res := <-resChan:
		return res
	}
}

func (a *Agent) processToolResult(name, result string) *api.Part {
	// Multi-modal image injection logic
	if strings.HasPrefix(result, "MULTI_MODAL_IMAGE|") {
		parts := strings.SplitN(result, "|", 4)
		if len(parts) == 4 {
			mimeType := parts[1]
			b64Data := parts[2]
			displayMsg := parts[3]

			p := &api.Part{
				FunctionResponse: &api.FunctionResponse{
					Name:     name,
					Response: map[string]interface{}{"result": displayMsg},
				},
			}
			// Mark for injection in a temporary field we won't serialize
			p.Text = "INJECT:" + mimeType + ":" + b64Data
			return p
		}
	}

	return &api.Part{
		FunctionResponse: &api.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result},
		},
	}
}

func (a *Agent) injectBinaryData(parts []*api.Part) []*api.Part {
	var finalParts []*api.Part
	for _, p := range parts {
		if strings.HasPrefix(p.Text, "INJECT:") {
			injectParts := strings.SplitN(p.Text, ":", 3)
			p.Text = "" // Clear the marker
			if len(injectParts) == 3 {
				data, _ := base64.StdEncoding.DecodeString(injectParts[2])
				finalParts = append(finalParts, &api.Part{
					InlineData: &genai.Blob{
						MIMEType: injectParts[1],
						Data:     data,
					},
				})
			}
		}
		finalParts = append(finalParts, p)
	}
	return finalParts
}
