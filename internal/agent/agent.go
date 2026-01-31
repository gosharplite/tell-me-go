// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
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
	LogToolCall(calls []*types.FunctionCall, turn, maxTurns int, showTools bool)
	LogToolResult(name string, result types.ToolResult, showTools bool)
	LogSystemMessage(msg string, level string)
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
	executor             *ToolExecutor
	logFile              string
	showThoughts         bool
	showTools            bool
	rawOutput            bool
	persistentConfigPath string
	mainConfigPath       string
	startTime            time.Time
}

// New creates a new Agent.
func New(client types.LLMClient, hManager *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) *Agent {
	renderer := NewStdUIRenderer(sm)
	a := &Agent{
		client:         client,
		history:        hManager,
		registry:       registry,
		sm:             sm,
		renderer:       renderer,
		configWatcher:  NewConfigWatcher(120000, 10, 20),
		contextManager: NewContextManager(client, hManager, registry, sm),
		executor:       NewToolExecutor(registry, sm, hManager, renderer),
		showThoughts:   true,
		showTools:      true,
		rawOutput:      false,
		startTime:      time.Now(),
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
	a.executor.SetShowTools(showTools)
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
	a.executor.SetConcurrency(maxConcurrent, time.Duration(timeoutSeconds)*time.Second)
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
		a.executor.renderer = renderer
	}
}

func (a *Agent) refreshLimits() {
	a.configWatcher.Refresh()
	maxTokens, maxTurns, maxHistTurns := a.configWatcher.GetLimits()
	a.contextManager.SetLimits(maxTokens, maxTurns, maxHistTurns)
}

// Chat runs the multi-turn orchestration loop.
// Refactored to coordinate high-level phases: Prepare -> Generate -> Persist -> Execute -> Log.
func (a *Agent) Chat(ctx context.Context, prompt string) error {
	if err := a.history.AddContent(ctx, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	_, maxTurns, _ := a.contextManager.GetLimits()

	for turn := 0; turn <= maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.refreshLimits()

		// 1. Prepare API Contents (includes pruning, summarization, and safety warnings)
		apiContents, tokens, currentTurns, err := a.contextManager.PrepareContents(ctx, turn)
		if err != nil {
			return err
		}

		a.logTurnStatus(currentTurns, tokens, nil, false)

		// 2. Generate Response (Stream/Non-stream + Auth Retry)
		respContent, metrics, err := a.generateResponse(ctx, apiContents)
		if err != nil {
			return err
		}

		// 3. Persist Response
		if err := a.history.AddContent(ctx, respContent); err != nil {
			a.reportHistoryError(err)
		}

		// 4. Handle Tool Execution
		if err := a.handleToolExecution(ctx, respContent, turn, metrics); err != nil {
			return err
		}

		// Refresh limits to ensure tool updates (e.g., manage_config) are reflected immediately
		a.refreshLimits()
		a.logTurnStatus(currentTurns, tokens, metrics, true)

		if metrics != nil {
			a.renderer.LogUsage(metrics, a.logFile, a.startTime)
		}

		if !a.hasToolCalls(respContent) {
			break
		}
	}
	return nil
}

// generateResponse handles the LLM interaction logic, including streaming and auth retries.
func (a *Agent) generateResponse(ctx context.Context, apiContents []*types.Content) (*types.Content, *types.Metrics, error) {
	if os.Getenv("TELL_ME_NO_STREAM") == "true" {
		respContent, metrics, err := a.client.SendChat(ctx, apiContents, a.registry.GetDeclarations(), a.history.GetResolver())
		if err == nil {
			a.renderer.RenderResponse(respContent, a.showThoughts, a.rawOutput)
		}
		return respContent, metrics, err
	}

	streamCh, finalize := a.renderer.StreamResponse(ctx, a.showThoughts, a.rawOutput)
	metrics, err := a.client.StreamChat(ctx, apiContents, a.registry.GetDeclarations(), a.history.GetResolver(), func(c *types.Content) {
		streamCh <- c
	})
	respContent := finalize()

	// Handle 401 Unauthorized for streaming
	if err != nil && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "UNAUTHENTICATED")) {
		a.renderer.LogSystemMessage("Token expired. Refreshing auth and retrying...", "info")
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
	return respContent, metrics, err
}

// handleToolExecution delegates execution to the ToolExecutor and tracks timing.
func (a *Agent) handleToolExecution(ctx context.Context, respContent *types.Content, turn int, metrics *types.Metrics) error {
	toolStart := time.Now()
	_, maxToolTurns, _ := a.contextManager.GetLimits()

	err := a.executor.Execute(ctx, respContent, turn, maxToolTurns)

	if metrics != nil {
		metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return err
}

// logTurnStatus constructs the status object and logs it to the renderer.
func (a *Agent) logTurnStatus(currentTurns, tokens int, metrics *types.Metrics, isPost bool) {
	maxTokens, _, maxHistTurns := a.contextManager.GetLimits()
	a.renderer.LogTurnStatus(TurnStatus{
		Timestamp:        time.Now(),
		CurrentTurns:     currentTurns,
		MaxHistoryTurns:  maxHistTurns,
		Tokens:           tokens,
		MaxHistoryTokens: maxTokens,
		Metrics:          metrics,
		IsPostCall:       isPost,
		StartTime:        a.startTime,
	})
}


func (a *Agent) reportHistoryError(err error) {
	a.renderer.LogSystemMessage(fmt.Sprintf("Failed to persist history entry: %v", err), "warn")
}

func (a *Agent) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
