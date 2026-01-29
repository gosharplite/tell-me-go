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
	prunedTurns        int
	maxConcurrentTools int
	toolTimeout        time.Duration
	showThoughts       bool
	showTools          bool
	rawOutput          bool
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
		rawOutput:          false,
	}
}

// SetUIOptions sets the UI visibility options.
func (a *Agent) SetUIOptions(showThoughts, showTools bool) {
	a.showThoughts = showThoughts
	a.showTools = showTools
}

// SetRawOutput sets whether to output raw text or rendered markdown.
func (a *Agent) SetRawOutput(raw bool) {
	a.rawOutput = raw
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
	hColor := "\033[0;90m" // Dark Gray (Quiet when Hit > Miss)
	reset := "\033[0m"     // Default Foreground (High contrast Miss)
	if miss > m.CachedTokens {
		hColor = reset
	}
	gray := "\033[0;90m"

	tools.TerminalMutex.Lock()
	defer tools.TerminalMutex.Unlock()
	fmt.Fprintf(os.Stderr, "%s[%s] %sH: %d M: %d%s C: %d T: %d N: %d(%d%%) S: %d Th: %d %s[%s%.2fs%s]%s\n",
		gray, timestamp, hColor, m.CachedTokens, miss, gray, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, gray, reset, m.Duration, gray, reset)
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
		return "[SYSTEM NOTICE: You are approaching the operational turn limit. You have 3 turns remaining. Please begin finalizing your current task, update the scratchpad and task list with your status, and avoid starting any new multi-step operations.]"
	case 2:
		return "[URGENT SYSTEM NOTICE: Operational limit imminent. Only 2 turns remaining. You must prioritize completing the current objective or documenting progress. You MUST document unfinished sub-tasks in 'manage_tasks' now. New tool sequences will be cut off.]"
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

func (a *Agent) getHistoryTurnWarning(currentTurns int) string {
	if a.maxHistoryTurns <= 0 {
		return ""
	}

	// 1. Check for recent major pruning (Aggressive cleanup)
	if a.prunedTurns > 5 {
		msg := fmt.Sprintf("[URGENT SYSTEM NOTICE: A major history cleanup has occurred. To maintain performance and cache efficiency, the oldest %d turns of this conversation have been removed. You have lost significant recent context. You MUST refer to the 'manage_scratchpad' and read 'manage_tasks' to continue unfinished tasks and re-synchronize your internal state.]", a.prunedTurns)
		a.prunedTurns = 0 // Reset after warning once
		return msg
	}

	ratio := float64(currentTurns) / float64(a.maxHistoryTurns)
	if ratio >= 1.0 {
		return "[SYSTEM NOTICE: The history turn limit has been reached and the oldest messages in this conversation have been deleted. If you are missing previous context or architectural details, please refer to 'manage_scratchpad' and 'manage_tasks' for the latest status and pending tasks.]"
	} else if ratio > 0.95 {
		return "[URGENT SYSTEM NOTICE: Conversation history is at 95% of the turn limit. Pruning is imminent. The oldest messages in this thread will be DELETED after this turn. Move all essential long-term memory to the scratchpad and task list immediately.]"
	} else if ratio > 0.90 {
		return "[SYSTEM NOTICE: Conversation history is at 90% of the turn limit. To prevent loss of context during upcoming pruning, ensure critical architectural decisions and progress are documented in the scratchpad and 'manage_tasks'.]"
	}
	return ""
}

// SetPrunedTurns informs the agent how many turns were removed during startup.
func (a *Agent) SetPrunedTurns(n int) {
	a.prunedTurns = n
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(prompt string) error {
	a.history.AddContent(&api.Content{
		Role:  "user",
		Parts: []*api.Part{{Text: prompt}},
	})
	_ = a.history.Save() // Persist initial user prompt immediately

	for turn := 0; turn <= a.maxToolTurns; turn++ {
		contents := a.history.GetContents()

		// 0. Enforce history turn limit
		if a.maxHistoryTurns > 0 && len(contents) > a.maxHistoryTurns*2 {
			pruned := a.history.Prune(a.maxHistoryTurns)
			if pruned > 0 {
				a.prunedTurns += pruned
				contents = a.history.GetContents()
			}
		}

		tokens := a.estimatePayloadTokens(contents)

		// 1. Safety Check: MAX_HISTORY_TOKENS
		if tokens > a.maxHistoryTokens {
			a.handleLimitExceeded(tokens)
			return ErrContextLimitExceeded
		}

		// Calculate current turns
		currentTurns := len(contents) / 2
		a.logSystemStatus(currentTurns, tokens)

		// 2. Prepare API Contents with warnings
		apiContents := a.prepareAPIContents(contents, turn, tokens, currentTurns)

		// 3. Send Chat Request
		respContent, metrics, err := a.sendChat(apiContents)
		if metrics != nil {
			a.logUsage(metrics)
		}
		if err != nil {
			return err
		}

		// 4. Render Output
		a.renderResponse(respContent)
		a.history.AddContent(respContent)
		_ = a.history.Save() // SAVE 1: Capture model's response/tool calls

		// 5. Handle Tool Execution
		if err := a.handleToolExecution(respContent, turn); err != nil {
			return err
		}
		_ = a.history.Save() // SAVE 2: Capture results of the tool calls

		if !a.hasToolCalls(respContent) {
			break
		}
	}

	return nil
}

func (a *Agent) handleLimitExceeded(tokens int) {
	tools.TerminalMutex.Lock()
	defer tools.TerminalMutex.Unlock()

	fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Safety Error] Payload estimate (%d tokens) exceeds limit (%d)!\033[0m\n",
		time.Now().Format("15:04:05"), tokens, a.maxHistoryTokens)
	fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Rolling back history. Please reduce context or start a new session.\033[0m\n",
		time.Now().Format("15:04:05"))
	a.history.Rollback()
}

func (a *Agent) logSystemStatus(currentTurns, tokens int) {
	tokenColor := "\033[0m" // Default white
	if float64(tokens) > float64(a.maxHistoryTokens)*0.9 {
		tokenColor = "\033[0;31m" // Red if > 90%
	}
	gray := "\033[0;90m"
	reset := "\033[0m"

	tools.TerminalMutex.Lock()
	defer tools.TerminalMutex.Unlock()
	fmt.Fprintf(os.Stderr, "%s[%s] [System (%s%d%s/%d)] Payload: ~%s%d%s/%d tokens%s\n",
		gray, time.Now().Format("15:04:05"), reset, currentTurns, gray, a.maxHistoryTurns, tokenColor, tokens, gray, a.maxHistoryTokens, reset)
}

func (a *Agent) prepareAPIContents(contents []*api.Content, turn, tokens, currentTurns int) []*api.Content {
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
	if turnWarning := a.getHistoryTurnWarning(currentTurns); turnWarning != "" {
		if warning != "" {
			warning += "\n" + turnWarning
		} else {
			warning = turnWarning
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

		tools.TerminalMutex.Lock()
		fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Safety warning injected into volatile model context.\033[0m\n",
			time.Now().Format("15:04:05"))
		tools.TerminalMutex.Unlock()
	}
	return apiContents
}

func (a *Agent) sendChat(apiContents []*api.Content) (*api.Content, *api.Metrics, error) {
	toolsSDK := a.registry.ToToolSDK()
	respContent, metrics, err := a.client.SendChat(apiContents, toolsSDK)

	// Handle 401 Unauthorized
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
		tools.TerminalMutex.Lock()
		fmt.Fprintf(os.Stderr, "\033[0;90m[System] Token expired. Refreshing auth and retrying...\033[0m\n")
		tools.TerminalMutex.Unlock()
		if refreshErr := a.client.RefreshAuth(); refreshErr != nil {
			return nil, nil, fmt.Errorf("failed to refresh auth: %w (original error: %v)", refreshErr, err)
		}
		// Retry
		respContent, metrics, err = a.client.SendChat(apiContents, a.registry.ToToolSDK())
	}
	return respContent, metrics, err
}

func (a *Agent) renderResponse(respContent *api.Content) {
	for _, part := range respContent.Parts {
		if a.showThoughts && part.Thought && part.Text != "" {
			tools.TerminalMutex.Lock()
			fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", time.Now().Format("15:04:05"), part.Text)
			tools.TerminalMutex.Unlock()
		}
	}
	for _, part := range respContent.Parts {
		if part.Text != "" && !part.Thought {
			if a.rawOutput {
				fmt.Print(part.Text)
				if !strings.HasSuffix(part.Text, "\n") {
					fmt.Println()
				}
			} else {
				a.renderMarkdown(part.Text)
			}
		}
	}
}

func (a *Agent) hasToolCalls(content *api.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

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
		tools.TerminalMutex.Lock()
		fmt.Fprintf(os.Stderr, "\033[0;31m[%s] [Error] Maximum tool execution turns (%d) reached. Stopping to prevent infinite loop.\033[0m\n",
			time.Now().Format("15:04:05"), a.maxToolTurns)
		tools.TerminalMutex.Unlock()
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
	tools.TerminalMutex.Lock()
	defer tools.TerminalMutex.Unlock()

	var names []string
	for _, fc := range calls {
		names = append(names, fc.Name)
	}

	gray := "\033[0;90m"
	cyan := "\033[0;36m"
	reset := "\033[0m"

	fmt.Fprintf(os.Stderr, "%s[%s] %s[Tool Engine (%s%d%s/%d)] Calling: %s%s\n",
		gray, time.Now().Format("15:04:05"), cyan, reset, turn+1, cyan, a.maxToolTurns, strings.Join(names, ", "), reset)

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
	// Execute with timeout (exclude interactive tools)
	var ctx context.Context
	var cancel context.CancelFunc

	if call.Name == "ask_user" || call.Name == "execute_command" {
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
