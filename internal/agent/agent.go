// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"google.golang.org/genai"
)

// Agent represents the chat orchestration logic.
type Agent struct {
	client             *api.Client
	history            *history.Manager
	registry           *tools.Registry
	logFile            string
	maxToolTurns       int
	maxHistoryTokens   int
	maxConcurrentTools int
	toolTimeout        time.Duration
}

// New creates a new Agent.
func New(client *api.Client, hManager *history.Manager, registry *tools.Registry) *Agent {
	return &Agent{
		client:             client,
		history:            hManager,
		registry:           registry,
		maxToolTurns:       10,
		maxHistoryTokens:   120000,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
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

// SetConcurrency sets the parallel execution limits for the agent.
func (a *Agent) SetConcurrency(maxConcurrent int, timeoutSeconds int) {
	if maxConcurrent > 0 {
		a.maxConcurrentTools = maxConcurrent
	}
	if timeoutSeconds > 0 {
		a.toolTimeout = time.Duration(timeoutSeconds) * time.Second
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

	// Prepare colored line for stderr
	hColor := "\033[0;90m" // Dark Gray
	if miss > m.CachedTokens {
		hColor = "\033[0;37m" // Light Gray
	}
	dColor := "\033[0;37m" // Light Gray for duration
	gray := "\033[0;90m"
	reset := "\033[0m"

	fmt.Fprintf(os.Stderr, "%s[%s] %sH: %d M: %d%s C: %d T: %d N: %d(%d%%) S: %d Th: %d %s[%.2fs]%s\n",
		gray, timestamp, hColor, m.CachedTokens, miss, gray, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, dColor, m.Duration, reset)
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
				a.renderMarkdown(part.Text)
			}
		}

		// Parallel Tool Execution
		var functionCalls []*api.FunctionCall
		for _, part := range respContent.Parts {
			if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			}
		}

		if len(functionCalls) > 0 {
			if turn >= a.maxToolTurns {
				fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Error] Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.\033[0m\n",
					time.Now().Format("15:04:05"), a.maxToolTurns)
				break
			}

			fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [Tool Engine] Executing %d tools (Parallel: %d, Timeout: %v)...\033[0m\n",
				time.Now().Format("15:04:05"), len(functionCalls), a.maxConcurrentTools, a.toolTimeout)

			results := make([]*api.Part, len(functionCalls))
			var wg sync.WaitGroup
			sem := make(chan struct{}, a.maxConcurrentTools)

			for i, fc := range functionCalls {
				wg.Add(1)
				go func(idx int, call *api.FunctionCall) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [Tool Action] %s\033[0m\n", time.Now().Format("15:04:05"), call.Name)

					// Execute with timeout (exclude interactive tools)
					var ctx context.Context
					var cancel context.CancelFunc

					if call.Name == "ask_user" || call.Name == "execute_command" {
						// Interactive tools don't time out while waiting for user
						ctx = context.Background()
						cancel = func() {}
					} else {
						ctx, cancel = context.WithTimeout(context.Background(), a.toolTimeout)
					}
					defer cancel()

					resChan := make(chan string, 1)
					go func() {
						result, err := a.registry.Execute(call.Name, call.Args)
						if err != nil {
							resChan <- fmt.Sprintf("Error: %v", err)
						} else {
							resChan <- result
						}
					}()

					var finalResult string
					select {
					case <-ctx.Done():
						finalResult = fmt.Sprintf("Error: Tool execution timed out after %v", a.toolTimeout)
					case res := <-resChan:
						finalResult = res
					}

					// Multi-modal image injection logic
					if strings.HasPrefix(finalResult, "MULTI_MODAL_IMAGE|") {
						parts := strings.SplitN(finalResult, "|", 4)
						if len(parts) == 4 {
							mimeType := parts[1]
							b64Data := parts[2]
							displayMsg := parts[3]

							results[idx] = &api.Part{
								FunctionResponse: &api.FunctionResponse{
									Name:     call.Name,
									Response: map[string]interface{}{"result": displayMsg},
								},
							}
							// Mark for injection in a temporary field we won't serialize
							results[idx].Text = "INJECT:" + mimeType + ":" + b64Data
						} else {
							results[idx] = &api.Part{
								FunctionResponse: &api.FunctionResponse{
									Name:     call.Name,
									Response: map[string]interface{}{"result": finalResult},
								},
							}
						}
					} else {
						results[idx] = &api.Part{
							FunctionResponse: &api.FunctionResponse{
								Name:     call.Name,
								Response: map[string]interface{}{"result": finalResult},
							},
						}
					}
				}(i, fc)
			}
			wg.Wait()

			// Post-process multi-modal injections
			var responseParts []*api.Part
			var imageParts []*api.Part

			for _, p := range results {
				if strings.HasPrefix(p.Text, "INJECT:") {
					injectParts := strings.SplitN(p.Text, ":", 3)
					p.Text = "" // Clear the marker
					if len(injectParts) == 3 {
						data, _ := base64.StdEncoding.DecodeString(injectParts[2])
						imageParts = append(imageParts, &api.Part{
							InlineData: &genai.Blob{
								MIMEType: injectParts[1],
								Data:     data,
							},
						})
					}
				}
				responseParts = append(responseParts, p)
			}

			a.history.AddContent(&api.Content{
				Role:  "user",
				Parts: responseParts,
			})

			if len(imageParts) > 0 {
				a.history.AddContent(&api.Content{
					Role:  "user",
					Parts: imageParts,
				})
			}
			continue
		}

		break
	}

	return nil
}

func (a *Agent) renderMarkdown(text string) {
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)

	out, err := r.Render(text)
	if err != nil {
		fmt.Print(text)
	} else {
		fmt.Print(out)
	}
}
