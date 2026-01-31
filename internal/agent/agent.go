// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
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
	SetMainConfigPath(path string)
	SetRenderer(renderer UIRenderer)
}

// UIRenderer defines the interface for UI feedback.
type UIRenderer interface {
	RenderResponse(respContent *types.Content, showThoughts, rawOutput bool)
	StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *types.Content, func() *types.Content)
	LogTurnStatus(status TurnStatus)
	LogUsage(m *types.Metrics, logFile string, startTime time.Time)
	LogToolResult(name string, result types.ToolResult, showTools bool)
}

// TurnStatus contains the data needed for rendering turn status.
type TurnStatus struct {
	Timestamp        time.Time
	CurrentTurns     int
	MaxHistoryTurns  int
	Tokens           int
	MaxHistoryTokens int
	Metrics          *types.Metrics
	IsPostCall       bool
	StartTime        time.Time
}

// Agent represents the chat orchestration logic.
type Agent struct {
	client               types.LLMClient
	history              *history.Manager
	registry             *tools.Registry
	sm                   *tools.SecurityManager
	renderer             UIRenderer
	configWatcher        *ConfigWatcher
	contextManager       *ContextManager
	logFile              string
	maxConcurrentTools   int
	toolTimeout          time.Duration
	showThoughts         bool
	showTools            bool
	rawOutput            bool
	persistentConfigPath string
	mainConfigPath       string
	startTime            time.Time
}

// New creates a new Agent.
func New(client types.LLMClient, hManager *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) *Agent {
	a := &Agent{
		client:             client,
		history:            hManager,
		registry:           registry,
		sm:                 sm,
		renderer:           NewStdUIRenderer(sm),
		configWatcher:      NewConfigWatcher(120000, 10, 20),
		contextManager:     NewContextManager(client, hManager, registry, sm),
		maxConcurrentTools: 5,
		toolTimeout:        30 * time.Second,
		showThoughts:       true,
		showTools:          true,
		rawOutput:          false,
		startTime:          time.Now(),
	}
	a.registerInternalTools()
	a.refreshLimits() // Initial load
	return a
}

func (a *Agent) registerInternalTools() {
	a.registry.Register(&types.ToolDeclaration{
		Name:        "summarize_history",
		Description: "Summarizes a specified number of older conversation turns to free up context space.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"turns": {
					Type:        "NUMBER",
					Description: "The number of turns (user+model pairs) to summarize from the beginning of history.",
				},
			},
			Required: []string{"turns"},
		},
	}, a.contextManager.SummarizeHistoryTool)
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
	a.contextManager.SetLimits(historyTokens, toolTurns, historyTurns)
	a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
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
	a.contextManager.SetPrunedTurns(n)
}

// SetPersistentConfigPath sets the path to the persistent session configuration.
func (a *Agent) SetPersistentConfigPath(path string) {
	a.persistentConfigPath = path
	a.configWatcher.SetPaths(a.mainConfigPath, path)
}

// SetMainConfigPath sets the path to the main YAML configuration file.
func (a *Agent) SetMainConfigPath(path string) {
	a.mainConfigPath = path
	a.configWatcher.SetPaths(path, a.persistentConfigPath)
}

// SetRenderer sets a custom UI renderer.
func (a *Agent) SetRenderer(renderer UIRenderer) {
	if renderer != nil {
		a.renderer = renderer
	}
}

func (a *Agent) refreshLimits() {
	a.configWatcher.Refresh()
	maxTokens, maxTurns, maxHistTurns := a.configWatcher.GetLimits()
	a.contextManager.SetLimits(maxTokens, maxTurns, maxHistTurns)

	if a.persistentConfigPath != "" {
		if data, err := os.ReadFile(a.persistentConfigPath); err == nil {
			var config map[string]string
			if err := json.Unmarshal(data, &config); err == nil {
				a.contextManager.SetSmartSuggestions(config["smart_suggestions"] == "on")
			}
		}
	}
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx context.Context, prompt string) error {
	if err := a.history.AddContent(&types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	_, maxTurns, _ := a.contextManager.GetLimits()

	for turn := 0; turn <= maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		a.refreshLimits()

		// 1. Prepare API Contents (includes pruning, summarization, and safety warnings)
		apiContents, tokens, currentTurns, err := a.contextManager.PrepareContents(ctx, turn)
		if err != nil {
			return err
		}

		maxTokens, _, maxHistTurns := a.contextManager.GetLimits()

		a.renderer.LogTurnStatus(TurnStatus{
			Timestamp:        time.Now(),
			CurrentTurns:     currentTurns,
			MaxHistoryTurns:  maxHistTurns,
			Tokens:           tokens,
			MaxHistoryTokens: maxTokens,
			IsPostCall:       false,
		})

		// 2. Send Chat Request (Streaming or Non-streaming)
		var metrics *types.Metrics
		var respContent *types.Content

		if os.Getenv("TELL_ME_NO_STREAM") == "true" {
			respContent, metrics, err = a.client.SendChat(ctx, apiContents, a.registry.GetDeclarations(), a.history.GetResolver())
			if err == nil {
				a.renderer.RenderResponse(respContent, a.showThoughts, a.rawOutput)
			}
		} else {
			streamCh, finalize := a.renderer.StreamResponse(ctx, a.showThoughts, a.rawOutput)
			metrics, err = a.client.StreamChat(ctx, apiContents, a.registry.GetDeclarations(), a.history.GetResolver(), func(c *types.Content) {
				streamCh <- c
			})
			respContent = finalize()

			// Handle 401 Unauthorized for streaming
			if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
				func() {
					a.sm.TerminalLock()
					defer a.sm.TerminalUnlock()
					fmt.Fprintf(os.Stderr, "\033[0;90m[System] Token expired. Refreshing auth and retrying...\033[0m\n")
				}()
				if refreshErr := a.client.RefreshAuth(); refreshErr == nil {
					// Finalize the failed stream before retrying to prevent goroutine leak
					_ = finalize()
					// Retry streaming
					streamCh, finalize = a.renderer.StreamResponse(ctx, a.showThoughts, a.rawOutput)
					metrics, err = a.client.StreamChat(ctx, apiContents, a.registry.GetDeclarations(), a.history.GetResolver(), func(c *types.Content) {
						streamCh <- c
					})
					respContent = finalize()
				}
			}
		}

		if err != nil {
			return err
		}

		// 3. Persist Response
		if err := a.history.AddContent(respContent); err != nil {
			a.reportHistoryError(err)
		}

		// 4. Handle Tool Execution
		toolStart := time.Now()
		err = a.handleToolExecution(ctx, respContent, turn)
		if metrics != nil {
			metrics.ToolDuration = time.Since(toolStart).Seconds()
		}
		if err != nil {
			return err
		}

		// Refresh limits to ensure tool updates (e.g. manage_config) are reflected in logs immediately
		a.refreshLimits()
		maxTokens, _, maxHistTurns = a.contextManager.GetLimits()

		a.renderer.LogTurnStatus(TurnStatus{
			Timestamp:        time.Now(),
			CurrentTurns:     currentTurns,
			MaxHistoryTurns:  maxHistTurns,
			Tokens:           tokens,
			MaxHistoryTokens: maxTokens,
			Metrics:          metrics,
			IsPostCall:       true,
			StartTime:        a.startTime,
		})
		if metrics != nil {
			a.renderer.LogUsage(metrics, a.logFile, a.startTime)
		}

		if !a.hasToolCalls(respContent) {
			break
		}
	}
	return nil
}

func (a *Agent) reportHistoryError(err error) {
	a.sm.TerminalLock()
	defer a.sm.TerminalUnlock()
	fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Warning] Failed to persist history entry: %v\033[0m\n",
		time.Now().Format("15:04:05"), err)
}

func (a *Agent) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
