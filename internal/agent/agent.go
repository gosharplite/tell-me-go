// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
)

// Agent represents the chat orchestration logic.
type Agent struct {
	client   *api.Client
	history  *history.Manager
	registry *tools.Registry
	logFile  string
}

// New creates a new Agent.
func New(client *api.Client, hManager *history.Manager, registry *tools.Registry) *Agent {
	return &Agent{
		client:   client,
		history:  hManager,
		registry: registry,
	}
}

// SetLogFile sets the path for usage logging.
func (a *Agent) SetLogFile(path string) {
	a.logFile = path
}

func (a *Agent) logUsage(m *api.Metrics) {
	if a.logFile == "" || m == nil {
		return
	}

	miss := m.PromptTokens - m.CachedTokens
	newTokens := miss + m.ResponseTokens + m.ThinkingTokens
	percent := 0
	if m.TotalTokens > 0 {
		percent = int((int64(newTokens) * 100) / int64(m.TotalTokens))
	}

	timestamp := time.Now().Format("15:04:05")
	// [Time] H: 0 M: 45201 C: 217 T: 46102 N: 45418(98%) S: 1 Th: 1540 [13.5s]
	logLine := fmt.Sprintf("[%s] H: %d M: %d C: %d T: %d N: %d(%d%%) S: %d Th: %d [%.2fs]\n",
		timestamp, m.CachedTokens, miss, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, m.Duration)

	// Append to log file
	f, err := os.OpenFile(a.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(logLine)

	// Print to stderr in gray
	fmt.Fprintf(os.Stderr, "\033[0;90m%s\033[0m", logLine)
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(prompt string) error {
	a.history.AddContent(&api.Content{
		Role:  "user",
		Parts: []*api.Part{{Text: prompt}},
	})

	for {
		// Tell-me style: log the start of the API call
		fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [API] Calling Gemini...\033[0m\n", time.Now().Format("15:04:05"))

		respContent, metrics, err := a.client.SendChat(a.history.GetContents(), a.registry.ToToolSDK())

		// Handle 401 Unauthorized (Expired Token)
		if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
			fmt.Fprintf(os.Stderr, "\033[0;90m[System] Token expired. Refreshing auth and retrying...\033[0m\n")
			if refreshErr := a.client.RefreshAuth(); refreshErr != nil {
				return fmt.Errorf("failed to refresh auth: %w (original error: %v)", refreshErr, err)
			}
			// Retry once
			respContent, metrics, err = a.client.SendChat(a.history.GetContents(), a.registry.ToToolSDK())
		}

		if err != nil {
			return err
		}

		// Log usage metrics
		a.logUsage(metrics)

		// 1. Check for thoughts and print them
		for _, part := range respContent.Parts {
			if part.Thought && part.Text != "" {
				fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", time.Now().Format("15:04:05"), part.Text)
			}
		}

		// 2. Add response to history
		a.history.AddContent(respContent)

		// 3. Check for function calls
		hasFunctionCall := false
		var functionResponseParts []*api.Part

		for _, part := range respContent.Parts {
			if part.FunctionCall != nil {
				hasFunctionCall = true
				fc := part.FunctionCall

				// Tell-me style: log the tool action
				fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [Tool Action] %s\033[0m\n", time.Now().Format("15:04:05"), fc.Name)

				// Execute the tool
				result, err := a.registry.Execute(fc.Name, fc.Args)
				if err != nil {
					// On error, we still send the error back to the model
					result = fmt.Sprintf("Error: %v", err)
				}

				functionResponseParts = append(functionResponseParts, &api.Part{
					FunctionResponse: &api.FunctionResponse{
						Name:     fc.Name,
						Response: map[string]interface{}{"result": result},
					},
				})
			}
		}

		if hasFunctionCall {
			// Add function responses to history as a "user" role (SDK requirement)
			a.history.AddContent(&api.Content{
				Role:  "user",
				Parts: functionResponseParts,
			})
			// Loop again to give the result to the model
			continue
		}

		// 4. No more function calls, print final text response
		for _, part := range respContent.Parts {
			if part.Text != "" && !part.Thought {
				fmt.Println(part.Text)
			}
		}
		break
	}

	return nil
}

