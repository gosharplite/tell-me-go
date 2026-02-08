// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	stdctx "context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/context"
	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/services/summarizer"
)

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	Chat(ctx stdctx.Context, s *Session, prompt string) error
	SetLimits(toolTurns, historyTokens, historyTurns int)
	SetHardBudgetLimit(limit float64)
	SetTieredThreshold(threshold int)
	SetPrunedTurns(n int)
	SetSystemInstructions(instr string)
	Subscribe(sub func(events.Event))
	GetCostTracker() domain_pricing.ICostTracker
}

// RuntimeConfig consolidates all agent configuration parameters.
type RuntimeConfig struct {
	Limits             events.Limits
	Model              string
	Mode               string
	PricingOverrides   map[string]pricing.ModelPricing
	HardBudgetLimit    float64
	SystemInstructions string
}

// Agent represents the chat orchestration logic (Stateless Service).
type Agent struct {
	mu            sync.RWMutex
	gateway       *gateway.ResilientClient
	engine        *TurnEngine
	ctxManager    *context.ContextManager
	registry      tools.IToolRegistry
	sm            security.ISecurityManager
	configWatcher *ConfigWatcher
	strategy      *context.ContextStrategy
	executor      *executor.ToolExecutor
	events        events.EventBus
	tracker       domain_pricing.ICostTracker

	config RuntimeConfig
}

// AgentOption defines a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithPricing sets the pricing configuration for cost estimation.
func WithPricing(model, mode string, overrides map[string]pricing.ModelPricing) AgentOption {
	return func(a *Agent) {
		a.config.Model = model
		a.config.Mode = mode
		a.config.PricingOverrides = overrides
	}
}

// WithLimits sets the initial operational limits.
func WithLimits(toolTurns, historyTokens, historyTurns int) AgentOption {
	return func(a *Agent) {
		a.config.Limits = events.Limits{
			MaxHistoryTokens: historyTokens,
			MaxToolTurns:     toolTurns,
			MaxHistoryTurns:  historyTurns,
		}
		if a.configWatcher != nil {
			a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
		}
	}
}

// New creates a new Agent using functional options.
func New(client llm.LLMClient, hManager *history.Manager, reg tools.IToolRegistry, sm security.ISecurityManager, disableStreaming bool, options ...AgentOption) *Agent {
	bus := &events.SimpleEventBus{}
	gw := gateway.NewResilientClient(client, disableStreaming)

	strategy := context.NewContextStrategy(context.NewHeuristicTokenCounter(reg), bus)
	exec := executor.NewToolExecutor(reg, sm, bus)

	a := &Agent{
		gateway:       gw,
		registry:      reg,
		sm:            sm,
		configWatcher: NewConfigWatcher(config.DefaultMaxHistoryTokens, config.DefaultMaxToolTurns, config.DefaultMaxHistoryTurns),
		strategy:      strategy,
		executor:      exec,
		events:        bus,
		config: RuntimeConfig{
			Limits: events.Limits{
				MaxHistoryTokens: config.DefaultMaxHistoryTokens,
				MaxToolTurns:     config.DefaultMaxToolTurns,
				MaxHistoryTurns:  config.DefaultMaxHistoryTurns,
			},
		},
	}

	// Apply options
	for _, opt := range options {
		opt(a)
	}

	// Sync initial system instructions to gateway
	if a.config.SystemInstructions != "" {
		a.gateway.SetSystemInstructions(a.config.SystemInstructions)
	}

	factory := &context.PipelineFactory{
		Registry:   reg,
		History:    hManager,
		Summarizer: summarizer.NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}

	ctxManager := context.NewContextManager(strategy, hManager, bus, factory)
	a.ctxManager = ctxManager

	// Initialize engine
	a.engine = NewTurnEngine(gw, exec, ctxManager, reg, bus,
		WithConfig(a.sm, a.config.Model, a.config.PricingOverrides),
		WithCostTracker(a.tracker),
	)

	a.registerInternalTools()
	a.applyConfig() // Broadcast initial config
	return a
}

func (a *Agent) applyConfig() {
	a.mu.Lock()
	a.configWatcher.Refresh(a.config.Model)

	tokens, toolTurns, histTurns := a.configWatcher.GetLimits()
	threshold := a.configWatcher.GetTieredThreshold()
	window := a.configWatcher.GetContextWindow()

	// Update config from watcher if it changed
	a.config.Limits.MaxHistoryTokens = tokens
	a.config.Limits.MaxToolTurns = toolTurns
	a.config.Limits.MaxHistoryTurns = histTurns
	a.config.Limits.TieredThreshold = threshold

	// Update strategy with latest context window
	if a.strategy != nil {
		a.strategy.SetContextWindow(window)
	}

	// Capture values for engine reconfiguration outside of lock
	model := a.config.Model
	overrides := a.config.PricingOverrides
	budget := a.config.HardBudgetLimit

	a.events.Publish(events.ConfigUpdated{
		Limits: a.config.Limits,
	})
	a.mu.Unlock()

	// Sync engine configuration
	if a.engine != nil {
		a.engine.Reconfigure(
			WithConfig(a.sm, model, overrides),
			WithHardBudget(budget),
			WithCostTracker(a.tracker),
		)
	}
}

func (a *Agent) Subscribe(sub func(events.Event)) {
	a.events.Subscribe(sub)
}

func (a *Agent) emit(e events.Event) {
	a.events.Publish(e)
}

func (a *Agent) registerInternalTools() {
	it := NewInternalTools(a.ctxManager)
	a.registry.Register(&tools.ToolDeclaration{
		Name:        "summarize_history",
		Description: "Summarizes a specified number of older conversation turns to free up context space.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, it.SummarizeHistory)

	a.registry.Register(&tools.ToolDeclaration{
		Name:        "manage_history",
		Description: "Manages conversation history by pinning or unpinning specific turns to protect them from summarization/pruning.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"action": {
					Type:        "STRING",
					Description: "The action to perform: 'pin' or 'unpin'.",
					Enum:        []string{"pin", "unpin"},
				},
				"index": {
					Type:        "NUMBER",
					Description: "The 0-based index of the turn to manage.",
				},
			},
			Required: []string{"action", "index"},
		},
	}, it.ManageHistory)
}

// SetLimits sets the operational limits for the agent.
func (a *Agent) SetLimits(toolTurns, historyTokens, historyTurns int) {
	a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
	a.applyConfig()
}

func (a *Agent) SetHardBudgetLimit(limit float64) {
	a.mu.Lock()
	a.config.HardBudgetLimit = limit
	a.mu.Unlock()
	a.applyConfig()
}

func (a *Agent) SetTieredThreshold(threshold int) {
	a.configWatcher.SetTieredThreshold(threshold)
	a.applyConfig()
}

// SetPrunedTurns (Legacy support - usually in Session)
func (a *Agent) SetPrunedTurns(n int) {
	a.strategy.SetPrunedTurns(n)
}

// Chat runs the multi-turn orchestration loop.
func (a *Agent) Chat(ctx stdctx.Context, s *Session, prompt string) error {
	if err := s.History.AddContent(ctx, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	a.applyConfig()
	a.emit(events.StatusUpdate{Message: "Starting chat...", Level: "info"})
	return a.engine.Run(ctx, s.StartTime)
}

// GetCostTracker returns the session cost tracker used by the agent's engine.
func (a *Agent) GetCostTracker() domain_pricing.ICostTracker {
	return a.engine.GetCostTracker()
}

// SetSystemInstructions updates the system instructions used by the context pipeline.
func (a *Agent) SetSystemInstructions(instr string) {
	a.mu.Lock()
	a.config.SystemInstructions = instr
	if a.gateway != nil {
		a.gateway.SetSystemInstructions(instr)
	}
	a.mu.Unlock()
	a.applyConfig()
}

// WithSystemInstructions sets the initial system instructions.
func WithSystemInstructions(instr string) AgentOption {
	return func(a *Agent) {
		a.config.SystemInstructions = instr
	}
}

// WithSessionCostTracker sets the cost tracker for the agent.
func WithSessionCostTracker(tracker domain_pricing.ICostTracker) AgentOption {
	return func(a *Agent) {
		a.tracker = tracker
		if a.engine != nil {
			a.engine.Reconfigure(WithCostTracker(tracker))
		}
	}
}
