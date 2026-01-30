// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

var (
	// ErrContextLimitExceeded is returned when the payload exceeds the safety threshold.
	ErrContextLimitExceeded = fmt.Errorf("payload estimate exceeds safety limit")

	// ErrMaxTurnsReached is returned when the model reaches the turn limit.
	ErrMaxTurnsReached = fmt.Errorf("maximum tool execution turns reached")
)

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	Chat(ctx context.Context, prompt string) error
	SetLogFile(path string)
	SetUIOptions(showThoughts, showTools bool)
	SetRawOutput(raw bool)
	SetLimits(toolTurns, historyTokens, historyTurns int)
	SetPrunedTurns(n int)
	SetConcurrency(maxConcurrent int, timeoutSeconds int)
	SetPersistentConfigPath(path string)
}

// Agent represents the chat orchestration logic.
type Agent struct {
	client             *api.Client
	history            *history.Manager
	registry           *tools.Registry
	sm                 *tools.SecurityManager
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
	persistentConfigPath string
	startTime          time.Time
}

// New creates a new Agent.
func New(client *api.Client, hManager *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) *Agent {
	a := &Agent{
		client:             client,
		history:            hManager,
		registry:           registry,
		sm:                 sm,
		maxToolTurns:       10,
		maxHistoryTokens:   120000,
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		showThoughts:       true,
		showTools:          true,
		rawOutput:          false,
		startTime:          time.Now(),
	}
	a.registerInternalTools()
	return a
}

func (a *Agent) registerInternalTools() {
	a.registry.Register(&genai.FunctionDeclaration{
		Name:        "summarize_history",
		Description: "Summarizes a specified number of older conversation turns to free up context space.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"turns": {
					Type:        genai.TypeNumber,
					Description: "The number of turns (user+model pairs) to summarize from the beginning of history.",
				},
			},
			Required: []string{"turns"},
		},
	}, a.summarizeHistory)
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

// SetPrunedTurns informs the agent how many turns were removed during startup.
func (a *Agent) SetPrunedTurns(n int) {
	a.prunedTurns = n
}

// SetPersistentConfigPath sets the path to the persistent session configuration.
func (a *Agent) SetPersistentConfigPath(path string) {
	a.persistentConfigPath = path
}

func (a *Agent) refreshLimits() {
	if a.persistentConfigPath == "" {
		return
	}
	data, err := os.ReadFile(a.persistentConfigPath)
	if err != nil {
		return
	}
	var pCfg map[string]string
	if err := json.Unmarshal(data, &pCfg); err != nil {
		return
	}

	// Allow overriding core limits dynamically
	if val, ok := pCfg["MAX_HISTORY_TOKENS"]; ok {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			a.maxHistoryTokens = limit
		}
	}
	if val, ok := pCfg["MAX_TOOL_TURNS"]; ok {
		if limit, err := strconv.Atoi(val); err == nil && limit > 0 {
			a.maxToolTurns = limit
		}
	}
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx context.Context, prompt string) error {
	a.history.AddContent(&types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: prompt}},
	})
	a.saveHistory() // Persist initial user prompt immediately

	for turn := 0; turn <= a.maxToolTurns; turn++ {
		a.refreshLimits()
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
		// Trigger auto-summarization at 90% of the limit to provide a safety buffer.
		if tokens > int(float64(a.maxHistoryTokens)*0.9) {
			// Try auto-summarization before giving up
			if err := a.autoSummarize(ctx); err == nil {
				contents = a.history.GetContents()
				tokens = a.estimatePayloadTokens(contents)
			}

			// After summarization, if we are still over the hard limit, abort.
			if tokens > a.maxHistoryTokens {
				a.handleLimitExceeded(tokens)
				return ErrContextLimitExceeded
			}
		}

		// Calculate current turns
		currentTurns := len(contents) / 2

		// 2. Prepare API Contents with warnings
		apiContents := a.prepareAPIContents(contents, turn, tokens, currentTurns)
		a.logTurnStatus(currentTurns, tokens, nil, false)

		// 3. Send Chat Request
		respContent, metrics, err := a.sendChat(ctx, apiContents)
		if metrics != nil {
			a.logUsage(metrics)
		}
		if err != nil {
			return err
		}

		// 4. Render Output
		a.renderResponse(respContent)
		a.history.AddContent(respContent)
		a.saveHistory() // SAVE 1: Capture model's response/tool calls

		// 5. Handle Tool Execution
		if err := a.handleToolExecution(ctx, respContent, turn); err != nil {
			return err
		}
		a.saveHistory() // SAVE 2: Capture results of the tool calls

		a.logTurnStatus(currentTurns, tokens, metrics, true)

		if !a.hasToolCalls(respContent) {
			break
		}
	}

	return nil
}

func (a *Agent) saveHistory() {
	if err := a.history.Save(); err != nil {
		a.sm.TerminalLock()
		fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Warning] Failed to persist history: %v\033[0m\n",
			time.Now().Format("15:04:05"), err)
		a.sm.TerminalUnlock()
	}
}

func (a *Agent) prepareAPIContents(contents []*types.Content, turn, tokens, currentTurns int) []*types.Content {
	apiContents := make([]*types.Content, len(contents))
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
		cloned := &types.Content{
			Role:  orig.Role,
			Parts: make([]*types.Part, len(orig.Parts)),
		}
		copy(cloned.Parts, orig.Parts)
		cloned.Parts = append(cloned.Parts, &types.Part{
			Text: "\n\n" + warning,
		})
		apiContents[lastIdx] = cloned

		a.sm.TerminalLock()
		fmt.Fprintf(os.Stderr, "\033[0;33m[%s] [System] Safety warning injected into volatile model context.\033[0m\n",
			time.Now().Format("15:04:05"))
		a.sm.TerminalUnlock()
	}
	return apiContents
}

func (a *Agent) sendChat(ctx context.Context, apiContents []*types.Content) (*types.Content, *types.Metrics, error) {
	toolsSDK := a.registry.ToToolSDK()
	respContent, metrics, err := a.client.SendChat(ctx, apiContents, toolsSDK)

	// Handle 401 Unauthorized
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
		a.sm.TerminalLock()
		fmt.Fprintf(os.Stderr, "\033[0;90m[System] Token expired. Refreshing auth and retrying...\033[0m\n")
		a.sm.TerminalUnlock()
		if refreshErr := a.client.RefreshAuth(); refreshErr != nil {
			return nil, nil, fmt.Errorf("failed to refresh auth: %w (original error: %v)", refreshErr, err)
		}
		// Retry
		respContent, metrics, err = a.client.SendChat(ctx, apiContents, a.registry.ToToolSDK())
	}
	return respContent, metrics, err
}

func (a *Agent) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
