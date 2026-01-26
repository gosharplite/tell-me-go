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

var (
	// ErrContextLimitExceeded is returned when the payload exceeds the safety threshold.
	ErrContextLimitExceeded = fmt.Errorf("payload estimate exceeds safety limit")

	// ErrMaxTurnsReached is returned when the model reaches the turn limit.
	ErrMaxTurnsReached = fmt.Errorf("maximum tool execution turns reached")
)

// Agent represents the chat orchestration logic.
type Agent struct {
	client             *api.Client
	history            *history.Manager
	registry           *tools.Registry
	logFile            string
	maxToolTurns       int
	maxHistoryTokens   int
	maxHistoryTurns    int
	maxConcurrentTools int
	toolTimeout        time.Duration
	showThoughts       bool
	showTools          bool
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
		showThoughts:       true,
		showTools:          true,
	}
}

// SetUIOptions sets the UI visibility options.
func (a *Agent) SetUIOptions(showThoughts, showTools bool) {
	a.showThoughts = showThoughts
	a.showTools = showTools
}

// SetLimits sets the operational limits for the agent.
func (a *Agent) SetLimits(toolTurns, historyTokens, historyTurns int) {
	if toolTurns > 0 {
		a.maxToolTurns = toolTurns
	}
	if historyTokens > 0 {
		a.maxHistoryTokens = historyTokens
	}
	if historyTurns > 0 {
		a.maxHistoryTurns = historyTurns
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

func (a *Agent) getTurnWarning(turn int) string {
	remaining := a.maxToolTurns - turn
	switch remaining {
	case 3:
		return "[SYSTEM NOTICE: You are approaching the operational turn limit. You have 3 turns remaining. Please begin finalizing your current task and avoid starting any new multi-step operations.]"
	case 2:
		return "[URGENT SYSTEM NOTICE: Operational limit imminent. Only 2 turns remaining. You must prioritize completing the current objective or documenting progress. New tool sequences will be cut off.]"
	case 1:
		return "[FINAL SYSTEM WARNING: This is your absolute final turn. Provide your final conclusion or progress summary now. Process execution will terminate after this response.]"
	default:
		return ""
	}
}

func (a *Agent) getTokenWarning(tokens int) string {
	ratio := float64(tokens) / float64(a.maxHistoryTokens)
	if ratio > 0.95 {
		return "[CRITICAL SYSTEM NOTICE: Conversation history is at 95% capacity. Immediate risk of session rollback. You must use 'manage_scratchpad' and 'manage_tasks' to save a summary of your work and plans NOW. Keep your response extremely brief.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: The conversation history is at 90% capacity. To avoid a session crash, please minimize large file reads. Use 'manage_scratchpad' and 'manage_tasks' to save your current progress and architectural notes now, in case a rollback occurs.]"
	}
	return ""
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(prompt string) error {
	a.history.AddContent(&api.Content{
		Role:  "user",
		Parts: []*api.Part{{Text: prompt}},
	})

	for turn := 0; turn <= a.maxToolTurns; turn++ {
		contents := a.history.GetContents()
		tokens := a.estimatePayloadTokens(contents)

		// 1. Safety Check: MAX_HISTORY_TOKENS
		if tokens > a.maxHistoryTokens {
			fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Safety Error] Payload estimate (%d tokens) exceeds limit (%d)!\033[0m\n",
				time.Now().Format("15:04:05"), tokens, a.maxHistoryTokens)
			fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Rolling back history. Please reduce context or start a new session.\033[0m\n",
				time.Now().Format("15:04:05"))
			a.history.Rollback()
			return ErrContextLimitExceeded
		}

		// Calculate current turns (1 turn = user + model pair)
		currentTurns := len(contents) / 2
		tokenColor := "\033[0;90m" // Default dark gray
		if float64(tokens) > float64(a.maxHistoryTokens)*0.9 {
			tokenColor = "\033[0;31m" // Red if > 90%
		}
		fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [System (%d/%d)] Payload: ~%s%d\033[0;90m tokens\033[0m\n",
			time.Now().Format("15:04:05"), currentTurns, a.maxHistoryTurns, tokenColor, tokens)

		// 2. Prepare API Contents (Deep copy to avoid history pollution via warnings)
		apiContents := make([]*api.Content, len(contents))
		copy(apiContents, contents)

		warning := a.getTurnWarning(turn)
		if tokenWarning := a.getTokenWarning(tokens); tokenWarning != "" {
			if warning != "" {
				warning += "\n" + tokenWarning
			} else {
				warning = tokenWarning
			}
		}

		if warning != "" && len(apiContents) > 0 {
			lastIdx := len(apiContents) - 1
			orig := apiContents[lastIdx]
			// Clone only the content that receives the warning
			cloned := &api.Content{
				Role:  orig.Role,
				Parts: make([]*api.Part, len(orig.Parts)),
			}
			copy(cloned.Parts, orig.Parts)
			cloned.Parts = append(cloned.Parts, &api.Part{
				Text: "\n\n" + warning,
			})
			apiContents[lastIdx] = cloned

			fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Safety warning injected into volatile model context.\033[0m\n",
				time.Now().Format("15:04:05"))
		}

		toolsSDK := a.registry.ToToolSDK()
		respContent, metrics, err := a.client.SendChat(apiContents, toolsSDK)

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
			if a.showThoughts && part.Thought && part.Text != "" {
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
				return ErrMaxTurnsReached
			}

			var names []string
			for _, fc := range functionCalls {
				names = append(names, fc.Name)
			}
			fmt.Fprintf(os.Stderr, "\033[0;36m[%s] [Tool Engine (%d/%d)] Calling: %s\033[0m\n",
				time.Now().Format("15:04:05"), turn+1, a.maxToolTurns, strings.Join(names, ", "))

			// Print all Tool Action headers BEFORE spawning goroutines to prevent UI interleaving
			if a.showTools {
				for _, fc := range functionCalls {
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

			results := make([]*api.Part, len(functionCalls))
			var wg sync.WaitGroup
			sem := make(chan struct{}, a.maxConcurrentTools)

			for i, fc := range functionCalls {
				wg.Add(1)
				go func(idx int, call *api.FunctionCall) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

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

			for _, p := range results {
				if strings.HasPrefix(p.Text, "INJECT:") {
					injectParts := strings.SplitN(p.Text, ":", 3)
					p.Text = "" // Clear the marker
					if len(injectParts) == 3 {
						data, _ := base64.StdEncoding.DecodeString(injectParts[2])
						responseParts = append(responseParts, &api.Part{
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
