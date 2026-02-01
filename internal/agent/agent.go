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
	Chat(ctx context.Context, s *Session, prompt string) error
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

// AgentConfig holds the configuration for the Agent.
type AgentConfig struct {
	LogFile              string
	ShowThoughts         bool
	ShowTools            bool
	RawOutput            bool
	PersistentConfigPath string
	MainConfigPath       string
}

// Agent represents the chat orchestration logic (Stateless Service).
type Agent struct {
	gateway       *gateway.ResilientClient
	engine        *TurnEngine
	ctxManager    *ContextManager
	registry      *registry.Registry
	sm            *tools.SecurityManager
	renderer      UIRenderer
	configWatcher *ConfigWatcher
	strategy      *ContextStrategy
	executor      *ToolExecutor
	events        EventBus

	config AgentConfig
}

// AgentOption defines a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithUIOptions sets the initial UI visibility options.
func WithUIOptions(showThoughts, showTools bool) AgentOption {
	return func(a *Agent) {
		a.config.ShowThoughts = showThoughts
		a.config.ShowTools = showTools
	}
}

// WithRawOutput sets whether to output raw text or rendered markdown.
func WithRawOutput(raw bool) AgentOption {
	return func(a *Agent) {
		a.config.RawOutput = raw
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
		a.config.LogFile = path
	}
}

// WithPrunedTurns (Legacy/Removed - state should be in Session)
func WithPrunedTurns(n int) AgentOption {
	return func(a *Agent) {
		// No-op or handle appropriately if still needed for init
	}
}

// WithPersistentConfigPath sets the path to the persistent session configuration.
func WithPersistentConfigPath(path string) AgentOption {
	return func(a *Agent) {
		a.config.PersistentConfigPath = path
		a.configWatcher.SetPaths(a.config.MainConfigPath, path)
	}
}

// WithMainConfigPath sets the path to the main YAML configuration file.
func WithMainConfigPath(path string) AgentOption {
	return func(a *Agent) {
		a.config.MainConfigPath = path
		a.configWatcher.SetPaths(path, a.config.PersistentConfigPath)
	}
}

// WithRenderer sets a custom UI renderer and automatically subscribes it to events.
func WithRenderer(renderer UIRenderer) AgentOption {
	return func(a *Agent) {
		if renderer != nil {
			a.renderer = renderer
		}
	}
}

// New creates a new Agent using functional options.
func New(client types.LLMClient, hManager *history.Manager, reg *registry.Registry, sm *tools.SecurityManager, disableStreaming bool, options ...AgentOption) *Agent {
	events := &SimpleEventBus{}
	renderer := NewStdUIRenderer(sm)
	gw := gateway.NewResilientClient(client, disableStreaming)
	strategy := NewContextStrategy(reg)
	executor := NewToolExecutor(reg, sm, events)
	ctxManager := NewContextManager(strategy, hManager, gw, events)

	a := &Agent{
		gateway:       gw,
		ctxManager:    ctxManager,
		registry:      reg,
		sm:            sm,
		renderer:      renderer,
		configWatcher: NewConfigWatcher(120000, 10, 20),
		strategy:      strategy,
		executor:      executor,
		events:        events,
		config: AgentConfig{
			ShowThoughts: true,
			ShowTools:    true,
			RawOutput:    false,
		},
	}

	// Apply options
	for _, opt := range options {
		opt(a)
	}

	// Initialize engine
	a.engine = NewTurnEngine(gw, executor, ctxManager, reg, events)

	// Subscribe renderer to events
	a.events.Subscribe(a.handleEvent)

	a.registerInternalTools()
	a.refreshLimits() // Initial load
	return a
}

func (a *Agent) handleEvent(e Event) {
	switch ev := e.(type) {
	case TurnStatusEvent:
		a.renderer.LogTurnStatus(ev.Status)
	case ResponseStreamEvent:
		uiCh, uiFinalize := a.renderer.StreamResponse(ev.Context, a.config.ShowThoughts, a.config.RawOutput)
		for c := range ev.Stream {
			uiCh <- c
		}
		_ = uiFinalize()
	case UsageMetricsEvent:
		a.renderer.LogUsage(ev.Metrics, a.config.LogFile, ev.StartTime)
	case ToolCallEvent:
		a.renderer.LogToolCall(ev.Calls, ev.Turn, ev.MaxTurns, a.config.ShowTools)
	case ToolResultEvent:
		a.renderer.LogToolResult(ev.Name, ev.Result, a.config.ShowTools)
	case SystemMessageEvent:
		a.renderer.LogSystemMessage(ev.Message, ev.Level)
	case TokenLimitReachedEvent:
		// Already handled by SystemMessageEvent in TokenGatekeeper for now,
		// but could be used for specific UI triggers.
	case StatusUpdate:
		a.renderer.LogSystemMessage(ev.Message, ev.Level)
	}
}

func (a *Agent) Subscribe(sub func(Event)) {
	a.events.Subscribe(sub)
}

func (a *Agent) emit(e Event) {
	a.events.Publish(e)
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
	a.config.ShowThoughts = showThoughts
	a.config.ShowTools = showTools
	a.executor.SetShowTools(showTools)
}

// SetRawOutput sets whether to output raw text or rendered markdown.
func (a *Agent) SetRawOutput(raw bool) {
	a.config.RawOutput = raw
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
	a.config.LogFile = path
}

// SetPrunedTurns (Legacy support - usually in Session)
func (a *Agent) SetPrunedTurns(n int) {
	a.strategy.SetPrunedTurns(n)
}

// SetPersistentConfigPath sets the path to the persistent session configuration.
func (a *Agent) SetPersistentConfigPath(path string) {
	a.config.PersistentConfigPath = path
	a.configWatcher.SetPaths(a.config.MainConfigPath, path)
}

// SetMainConfigPath sets the path to the main YAML configuration file.
func (a *Agent) SetMainConfigPath(path string) {
	a.config.MainConfigPath = path
	a.configWatcher.SetPaths(path, a.config.PersistentConfigPath)
}

// SetRenderer sets a custom UI renderer.
func (a *Agent) SetRenderer(renderer UIRenderer) {
	a.renderer = renderer
}

func (a *Agent) refreshLimits() {
	a.configWatcher.Refresh()
	maxTokens, maxTurns, maxHistTurns := a.configWatcher.GetLimits()
	a.strategy.SetLimits(maxTokens, maxTurns, maxHistTurns)
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx context.Context, s *Session, prompt string) error {
	if err := s.History.AddContent(ctx, &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	a.refreshLimits()
	a.emit(StatusUpdate{Message: "Starting chat...", Level: "info"})
	return a.engine.Run(ctx, s.StartTime)
}
