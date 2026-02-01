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

// Agent represents the chat orchestration logic (Orchestrator).
type Agent struct {
	gateway              *gateway.ResilientClient
	engine               *TurnEngine
	ctxManager           *ContextManager
	session              *Session
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

	subscribers []func(Event)
}

// AgentOption defines a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithUIOptions sets the initial UI visibility options.
func WithUIOptions(showThoughts, showTools bool) AgentOption {
	return func(a *Agent) {
		a.showThoughts = showThoughts
		a.showTools = showTools
	}
}

// WithRawOutput sets whether to output raw text or rendered markdown.
func WithRawOutput(raw bool) AgentOption {
	return func(a *Agent) {
		a.rawOutput = raw
	}
}

// WithLimits sets the initial operational limits.
func WithLimits(toolTurns, historyTokens, historyTurns int) AgentOption {
	return func(a *Agent) {
		a.strategy.SetLimits(historyTokens, toolTurns, historyTurns)
		a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
	}
}

// WithConcurrency sets the parallel execution limits for the agent.
func WithConcurrency(maxConcurrent int, timeoutSeconds int) AgentOption {
	return func(a *Agent) {
		a.executor.SetConcurrency(maxConcurrent, time.Duration(timeoutSeconds)*time.Second)
	}
}

// WithLogFile sets the path for usage logging.
func WithLogFile(path string) AgentOption {
	return func(a *Agent) {
		a.logFile = path
	}
}

// WithPrunedTurns informs the agent how many turns were removed during startup.
func WithPrunedTurns(n int) AgentOption {
	return func(a *Agent) {
		a.session.PrunedTurns = n
		a.strategy.SetPrunedTurns(n)
	}
}

// WithPersistentConfigPath sets the path to the persistent session configuration.
func WithPersistentConfigPath(path string) AgentOption {
	return func(a *Agent) {
		a.persistentConfigPath = path
		a.configWatcher.SetPaths(a.mainConfigPath, path)
	}
}

// WithMainConfigPath sets the path to the main YAML configuration file.
func WithMainConfigPath(path string) AgentOption {
	return func(a *Agent) {
		a.mainConfigPath = path
		a.configWatcher.SetPaths(path, a.persistentConfigPath)
	}
}

// WithRenderer sets a custom UI renderer and automatically subscribes it to events.
func WithRenderer(renderer UIRenderer) AgentOption {
	return func(a *Agent) {
		if renderer != nil {
			a.renderer = renderer
			a.executor.renderer = renderer
		}
	}
}

// New creates a new Agent using functional options.
func New(client types.LLMClient, hManager *history.Manager, reg *registry.Registry, sm *tools.SecurityManager, disableStreaming bool, options ...AgentOption) *Agent {
	renderer := NewStdUIRenderer(sm)
	gw := gateway.NewResilientClient(client, disableStreaming)
	strategy := NewContextStrategy(reg)
	executor := NewToolExecutor(reg, sm, renderer)
	ctxManager := NewContextManager(strategy, hManager, gw, renderer)

	a := &Agent{
		gateway:       gw,
		ctxManager:    ctxManager,
		session:       NewSession(hManager),
		registry:      reg,
		sm:            sm,
		renderer:      renderer,
		configWatcher: NewConfigWatcher(120000, 10, 20),
		strategy:      strategy,
		executor:      executor,
		showThoughts:  true,
		showTools:     true,
		rawOutput:     false,
	}

	// Apply options
	for _, opt := range options {
		opt(a)
	}

	// Initialize engine with hooks that emit events
	a.engine = NewTurnEngine(gw, executor, ctxManager, reg, WithHooks(TurnHooks{
		OnTurnStart: func(turn int) {
			a.refreshLimits()
			a.emit(TurnStarted{Turn: turn})
		},
		OnPrepare: func(metadata *ContextMetadata) {
			status := a.getTurnStatus(metadata.FinalTurnCount, metadata.FinalTokenCount, nil, false)
			a.emit(TurnStatusEvent{Status: status})
			// Legacy support
			a.renderer.LogTurnStatus(status)
		},
		OnStream: func(ctx context.Context, respCh <-chan *types.Content) {
			a.emit(ResponseStreamEvent{Context: ctx, Stream: respCh})
			// Legacy support
			uiCh, uiFinalize := a.renderer.StreamResponse(ctx, a.showThoughts, a.rawOutput)
			for c := range respCh {
				uiCh <- c
			}
			_ = uiFinalize()
		},
		OnComplete: func(state *TurnState) {
			status := a.getTurnStatus(state.CurrentTurns, state.Tokens, state.Metrics, true)
			a.emit(TurnStatusEvent{Status: status})
			// Legacy support
			a.renderer.LogTurnStatus(status)
			if state.Metrics != nil {
				a.emit(UsageMetricsEvent{Metrics: state.Metrics, LogFile: a.logFile, StartTime: a.session.StartTime})
				a.renderer.LogUsage(state.Metrics, a.logFile, a.session.StartTime)
			}
		},
		OnResponse: func(content *types.Content) {
			// Events could be added here for tool call detection etc if needed
		},
		OnToolResults: func(results *types.Content) {
			// Events for individual tool results are currently handled by executor
		},
	}))

	a.registerInternalTools()
	a.refreshLimits() // Initial load
	return a
}

func (a *Agent) Subscribe(sub func(Event)) {
	a.subscribers = append(a.subscribers, sub)
}

func (a *Agent) emit(e Event) {
	for _, sub := range a.subscribers {
		sub(e)
	}
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
	WithUIOptions(showThoughts, showTools)(a)
	a.executor.SetShowTools(showTools)
}

// SetRawOutput sets whether to output raw text or rendered markdown.
func (a *Agent) SetRawOutput(raw bool) {
	WithRawOutput(raw)(a)
}

// SetLimits sets the operational limits for the agent.
func (a *Agent) SetLimits(toolTurns, historyTokens, historyTurns int) {
	WithLimits(toolTurns, historyTokens, historyTurns)(a)
}

// SetConcurrency sets the parallel execution limits for the agent.
func (a *Agent) SetConcurrency(maxConcurrent int, timeoutSeconds int) {
	WithConcurrency(maxConcurrent, timeoutSeconds)(a)
}

// SetLogFile sets the path for usage logging.
func (a *Agent) SetLogFile(path string) {
	WithLogFile(path)(a)
}

// SetPrunedTurns informs the agent how many turns were removed during startup.
func (a *Agent) SetPrunedTurns(n int) {
	WithPrunedTurns(n)(a)
}

// SetPersistentConfigPath sets the path to the persistent session configuration.
func (a *Agent) SetPersistentConfigPath(path string) {
	WithPersistentConfigPath(path)(a)
}

// SetMainConfigPath sets the path to the main YAML configuration file.
func (a *Agent) SetMainConfigPath(path string) {
	WithMainConfigPath(path)(a)
}

// SetRenderer sets a custom UI renderer.
func (a *Agent) SetRenderer(renderer UIRenderer) {
	WithRenderer(renderer)(a)
}

func (a *Agent) refreshLimits() {
	a.configWatcher.Refresh()
	maxTokens, maxTurns, maxHistTurns := a.configWatcher.GetLimits()
	a.strategy.SetLimits(maxTokens, maxTurns, maxHistTurns)
}

func (a *Agent) getTurnStatus(currentTurns, tokens int, metrics *types.Metrics, isPost bool) TurnStatus {
	maxTokens, _, maxHistTurns := a.strategy.GetLimits()
	return TurnStatus{
		Timestamp:        time.Now(),
		CurrentTurns:     currentTurns,
		MaxHistoryTurns:  maxHistTurns,
		Tokens:           tokens,
		MaxHistoryTokens: maxTokens,
		Metrics:          metrics,
		IsPostCall:       isPost,
		StartTime:        a.session.StartTime,
	}
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx context.Context, prompt string) error {
	if err := a.session.History.AddContent(ctx, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	a.refreshLimits()
	a.emit(StatusUpdate{Message: "Starting chat...", Level: "info"})
	return a.engine.Run(ctx, a.session.StartTime)
}
