// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"encoding/json"
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
	client           *api.Client
	history          *history.Manager
	registry         *tools.Registry
	logFile          string
	maxToolTurns     int
	maxHistoryTokens int
}

// New creates a new Agent.
func New(client *api.Client, hManager *history.Manager, registry *tools.Registry) *Agent {
	return &Agent{
		client:           client,
		history:          hManager,
		registry:         registry,
		maxToolTurns:     10,
		maxHistoryTokens: 120000,
	}
}

// SetLimits sets the operational limits for the agent.
func (a *Agent) SetLimits(toolTurns, historyTokens int) {
	if toolTurns > 0 {
		a.maxToolTurns = toolTurns
	}
	if historyTokens > 0 {
		a.maxHistoryTokens = historyTokens
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

func (a *Agent) estimatePayloadTokens(contents []*api.Content) int {
	charCount := 0

	// 1. Tool Declarations overhead
	for _, decl := range a.registry.GetDeclarations() {
		charCount += len(decl.Name) + len(decl.Description)
		if decl.Parameters != nil {
			charCount += 100
		}
	}

	// 2. Conversation History
	for _, c := range contents {
		for _, p := range c.Parts {
			if p.Text != "" {
				charCount += len(p.Text)
			}
			if p.FunctionCall != nil {
				charCount += len(p.FunctionCall.Name)
				if b, err := json.Marshal(p.FunctionCall.Args); err == nil {
					charCount += len(b)
				}
			}
			if p.FunctionResponse != nil {
				charCount += len(p.FunctionResponse.Name)
				if b, err := json.Marshal(p.FunctionResponse.Response); err == nil {
					charCount += len(b)
				}
			}
		}
	}

	// 3. Heuristic Adjustments
	charCount += 1000
	return int(float64(charCount) / 3.2)
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(prompt string) error {
	a.history.AddContent(&api.Content{
		Role:  "user",
		Parts: []*api.Part{{Text: prompt}},
	})

	for turn := 0; turn <= a.maxToolTurns; turn++ {
		contents := a.history.GetContents()
		toolsSDK := a.registry.ToToolSDK()

		tokens := a.estimatePayloadTokens(contents)

		// Log the payload info
		fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [System] Payload: ~%d tokens\033[0m\n",
			time.Now().Format("15:04:05"), tokens)

		// Safety Check: MAX_HISTORY_TOKENS
		if tokens > a.maxHistoryTokens {
			fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Safety Error] Payload estimate (%d tokens) exceeds limit (%d)!\033[0m\n",
				time.Now().Format("15:04:05"), tokens, a.maxHistoryTokens)
			fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Rolling back history. Please reduce context or start a new session.\033[0m\n",
				time.Now().Format("15:04:05"))
			a.history.Rollback()
			os.Exit(1)
		}

		respContent, metrics, err := a.client.SendChat(contents, toolsSDK)

		// Handle 401 Unauthorized
		if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
			fmt.Fprintf(os.Stderr, "\033[0;90m[System] Token expired. Refreshing auth and retrying...\033[0m\n")
			if refreshErr := a.client.RefreshAuth(); refreshErr != nil {
				return fmt.Errorf("failed to refresh auth: %w (original error: %v)", refreshErr, err)
			}
			respContent, metrics, err = a.client.SendChat(a.history.GetContents(), a.registry.ToToolSDK())
		}

		if metrics != nil {
			a.logUsage(metrics)
		}

		if err != nil {
			return err
		}

		for _, part := range respContent.Parts {
			if part.Thought && part.Text != "" {
				fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", time.Now().Format("15:04:05"), part.Text)
			}
		}

		a.history.AddContent(respContent)

		for _, part := range respContent.Parts {
			if part.Text != "" && !part.Thought {
				fmt.Println(part.Text)
			}
		}

		hasFunctionCall := false
		var functionResponseParts []*api.Part

		for _, part := range respContent.Parts {
			if part.FunctionCall != nil {
				hasFunctionCall = true
				fc := part.FunctionCall
				fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [Tool Action] %s\033[0m\n", time.Now().Format("15:04:05"), fc.Name)

				result, err := a.registry.Execute(fc.Name, fc.Args)
				if err != nil {
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
			if turn >= a.maxToolTurns {
				fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Error] Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.\033[0m\n",
					time.Now().Format("15:04:05"), a.maxToolTurns)
				break
			}
			a.history.AddContent(&api.Content{
				Role:  "user",
				Parts: functionResponseParts,
			})
			continue
		}

		break
	}

	return nil
}
