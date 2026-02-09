// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
)

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	Chat(ctx context.Context, s *Session, prompt string) error
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
	gateway       *llm.ResilientClient
	engine        *TurnEngine
	ctxManager    *orchestration.ContextManager
	registry      tools.IToolRegistry
	sm            domain_security.ISecurityManager
	configWatcher *ConfigWatcher
	strategy      *orchestration.ContextStrategy
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
func New(client domain_llm.LLMClient, hManager services.HistoryManager, reg tools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, options ...AgentOption) *Agent {
	bus := &events.SimpleEventBus{}
	gw := llm.NewResilientClient(client, disableStreaming)

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), bus)
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

	factory := &orchestration.PipelineFactory{
		Registry:   reg,
		History:    hManager,
		Summarizer: llm.NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}

	ctxManager := orchestration.NewContextManager(strategy, hManager, bus, factory)
	a.ctxManager = ctxManager

	// Initialize engine
	a.engine = NewTurnEngine(gw, exec, ctxManager, reg, bus,
		WithConfig(a.sm, a.config.Model, a.config.PricingOverrides),
		WithCostTracker(a.tracker),
	)

	orchestration.RegisterInternal(reg, ctxManager)
	a.applyConfig() // Broadcast initial config
	return a
}

func (a *Agent) applyConfig() {
	a.mu.Lock()
	a.configWatcher.Refresh(a.config.Model)

	tokens, toolTurns, histTurns := a.configWatcher.GetLimits()
	a.config.Limits.MaxHistoryTokens = tokens
	a.config.Limits.MaxToolTurns = toolTurns
	a.config.Limits.MaxHistoryTurns = histTurns
	a.config.Limits.TieredThreshold = a.configWatcher.GetTieredThreshold()

	if a.strategy != nil {
		a.strategy.SetContextWindow(a.configWatcher.GetContextWindow())
	}

	cfg := a.config
	tracker := a.tracker
	a.mu.Unlock()

	a.events.Publish(events.ConfigUpdated{Limits: cfg.Limits})

	if a.engine != nil {
		a.engine.Reconfigure(cfg, tracker)
	}
	if a.ctxManager != nil {
		a.ctxManager.Reconfigure(cfg.Limits)
	}
}

func (a *Agent) Subscribe(sub func(events.Event)) {
	a.events.Subscribe(sub)
}

func (a *Agent) emit(e events.Event) {
	a.events.Publish(e)
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
func (a *Agent) Chat(ctx context.Context, s *Session, prompt string) error {
	if err := s.History.AddContent(ctx, &domain_llm.Content{
		Role:  "user",
		Parts: []*domain_llm.Part{{Text: prompt}},
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
			a.engine.ApplyOptions(WithCostTracker(tracker))
		}
	}
}
