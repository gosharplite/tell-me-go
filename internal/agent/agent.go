// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
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
	gateway              *gateway.ResilientClient
	engine               *TurnEngine
	ctxManager           *ContextManager
	history              *history.Manager
	registry             *registry.Registry
	sm                   *tools.SecurityManager
	renderer             UIRenderer
	configWatcher        *ConfigWatcher
	strategy             *ContextStrategy
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
func New(client types.LLMClient, hManager *history.Manager, reg *registry.Registry, sm *tools.SecurityManager, disableStreaming bool) *Agent {
	renderer := NewStdUIRenderer(sm)
	gw := gateway.NewResilientClient(client, disableStreaming)
	strategy := NewContextStrategy(reg)
	executor := NewToolExecutor(reg, sm, renderer)
	ctxManager := NewContextManager(strategy, hManager, gw, renderer)
	engine := NewTurnEngine(gw, executor, ctxManager, renderer, reg)

	a := &Agent{
		gateway:       gw,
		engine:        engine,
		ctxManager:    ctxManager,
		history:       hManager,
		registry:      reg,
		sm:            sm,
		renderer:      renderer,
		configWatcher: NewConfigWatcher(120000, 10, 20),
		strategy:      strategy,
		executor:      executor,
		showThoughts:  true,
		showTools:     true,
		rawOutput:     false,
		startTime:     time.Now(),
	}
	a.engine.OnTurnStart = a.refreshLimits
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
				"focus": {
					Type:        "STRING",
					Description: "Optional: Specific aspects to focus on in the summary (e.g., 'architecture decisions').",
				},
			},
			Required: []string{"turns"},
		},
	}, a.ctxManager.SummarizeHistoryTool)
}

// SetUIOptions sets the UI visibility options.
func (a *Agent) SetUIOptions(showThoughts, showTools bool) {
	a.showThoughts = showThoughts
	a.showTools = showTools
	a.executor.SetShowTools(showTools)
	a.engine.SetUIOptions(showThoughts, a.rawOutput)
}

// SetRawOutput sets whether to output raw text or rendered markdown.
func (a *Agent) SetRawOutput(raw bool) {
	a.rawOutput = raw
	a.engine.SetUIOptions(a.showThoughts, raw)
}

// SetLimits sets the operational limits for the agent.
func (a *Agent) SetLimits(toolTurns, historyTokens, historyTurns int) {
	a.strategy.SetLimits(historyTokens, toolTurns, historyTurns)
	a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
}

// SetConcurrency sets the parallel execution limits for the agent.
func (a *Agent) SetConcurrency(maxConcurrent int, timeoutSeconds int) {
	a.executor.SetConcurrency(maxConcurrent, time.Duration(timeoutSeconds)*time.Second)
}

// SetLogFile sets the path for usage logging.
func (a *Agent) SetLogFile(path string) {
	a.logFile = path
	a.engine.SetLogFile(path)
}

// SetPrunedTurns informs the agent how many turns were removed during startup.
func (a *Agent) SetPrunedTurns(n int) {
	a.strategy.SetPrunedTurns(n)
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
		a.engine.renderer = renderer
	}
}

func (a *Agent) refreshLimits() {
	a.configWatcher.Refresh()
	maxTokens, maxTurns, maxHistTurns := a.configWatcher.GetLimits()
	a.strategy.SetLimits(maxTokens, maxTurns, maxHistTurns)
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx context.Context, prompt string) error {
	if err := a.history.AddContent(ctx, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	a.refreshLimits()
	return a.engine.Run(ctx, a.startTime)
}
