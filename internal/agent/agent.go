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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	// Chat runs the multi-turn orchestration loop.
	// It returns an error if the conversation cannot be initialized or the engine fails.
	Chat(ctx context.Context, s *orchestration.Session, prompt string) error

	// SetLimits sets the operational limits for the agent.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error

	// SetHardBudgetLimit sets the hard budget limit for the agent.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetHardBudgetLimit(ctx context.Context, limit float64) error

	// SetTieredThreshold sets the tiered threshold for the agent.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetTieredThreshold(ctx context.Context, threshold int) error

	// SetPrunedTurns sets the number of turns to prune from history.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetPrunedTurns(ctx context.Context, n int) error

	// SetSystemInstructions updates the system instructions used by the context pipeline.
	// It returns an error if the configuration cannot be applied (e.g., context cancellation).
	SetSystemInstructions(ctx context.Context, instr string) error

	// Subscribe adds a subscriber for agent events.
	Subscribe(sub func(events.Event))

	// GetCostTracker returns the session cost tracker.
	GetCostTracker() domain_pricing.ICostTracker

	// Shutdown gracefully stops the agent and its components.
	Shutdown(ctx context.Context) error
}

// runtimeConfig consolidates all agent configuration parameters.
type runtimeConfig struct {
	Limits             events.Limits
	Model              string
	Mode               string
	PricingOverrides   map[string]domain_pricing.ModelPricing
	HardBudgetLimit    float64
	SystemInstructions string
}

// Agent represents the chat orchestration logic (Stateless Service).
type agent struct {
	mu            sync.RWMutex
	gateway       *llm.ResilientClient
	engine        *turnEngine
	ctxManager    *orchestration.ContextManager
	registry      tools.IToolRegistry
	sm            domain_security.ISecurityManager
	configWatcher *orchestration.ConfigWatcher
	strategy      *orchestration.ContextStrategy
	executor      *executor.ToolExecutor
	events        events.EventBus
	tracker       domain_pricing.ICostTracker

	config           runtimeConfig
	registerInternal bool
}

// agentOption defines a functional option for configuring an Agent.
type agentOption func(*agent)

// WithPricing sets the pricing configuration for cost estimation.
func WithPricing(model, mode string, overrides map[string]domain_pricing.ModelPricing) agentOption {
	return func(a *agent) {
		a.config.Model = model
		a.config.Mode = mode
		a.config.PricingOverrides = overrides
	}
}

// WithLimits sets the initial operational limits.
func WithLimits(toolTurns, historyTokens, historyTurns int) agentOption {
	return func(a *agent) {
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

// WithInternalTools enables the registration of internal agent tools (e.g., history management).
func WithInternalTools() agentOption {
	return func(a *agent) {
		a.registerInternal = true
	}
}

// New creates a new Agent using functional options.
func New(client domain_llm.LLMClient, hManager services.HistoryManager, reg tools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, options ...agentOption) *agent {
	gw := llm.NewResilientClient(client, disableStreaming)

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), bus)
	exec := executor.NewToolExecutor(reg, sm, bus)

	a := &agent{
		gateway:       gw,
		registry:      reg,
		sm:            sm,
		configWatcher: orchestration.NewConfigWatcher(config.DefaultMaxHistoryTokens, config.DefaultMaxToolTurns, config.DefaultMaxHistoryTurns),
		strategy:      strategy,
		executor:      exec,
		events:        bus,
		config: runtimeConfig{
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
	a.engine = newTurnEngine(gw, exec, ctxManager, reg, bus,
		withConfig(a.sm, a.config.Model, a.config.PricingOverrides),
		withCostTracker(a.tracker),
	)

	if a.registerInternal {
		orchestration.RegisterInternal(reg, ctxManager)
	}

	if err := a.applyConfig(context.Background()); err != nil {
		a.emit(events.StatusUpdate{Message: "failed to apply initial configuration", Level: "warning"})
	}
	return a
}

func (a *agent) applyConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	a.mu.Lock()
	a.configWatcher.Refresh(a.config.Model)

	tokens, toolTurns, histTurns, threshold := a.configWatcher.GetLimits()
	a.config.Limits.MaxHistoryTokens = tokens
	a.config.Limits.MaxToolTurns = toolTurns
	a.config.Limits.MaxHistoryTurns = histTurns
	a.config.Limits.TieredThreshold = threshold

	if a.strategy != nil {
		a.configWatcher.SyncToStrategy(a.strategy)
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
	return nil
}

func (a *agent) Subscribe(sub func(events.Event)) {
	a.events.Subscribe(sub)
}

func (a *agent) emit(e events.Event) {
	a.events.Publish(e)
}

// SetLimits sets the operational limits for the agent.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
	return a.applyConfig(ctx)
}

// SetHardBudgetLimit sets the hard budget limit for the agent.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetHardBudgetLimit(ctx context.Context, limit float64) error {
	a.mu.Lock()
	a.config.HardBudgetLimit = limit
	a.mu.Unlock()
	return a.applyConfig(ctx)
}

// SetTieredThreshold sets the tiered threshold for the agent.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetTieredThreshold(ctx context.Context, threshold int) error {
	a.configWatcher.ApplyLimits(events.Limits{TieredThreshold: threshold})
	return a.applyConfig(ctx)
}

// SetPrunedTurns updates the number of turns to prune from history.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetPrunedTurns(ctx context.Context, n int) error {
	a.strategy.SetPrunedTurns(n)
	return a.applyConfig(ctx)
}

// Chat runs the multi-turn orchestration loop.
func (a *agent) Chat(ctx context.Context, s *orchestration.Session, prompt string) error {
	if err := a.ctxManager.AddContent(ctx, &domain_llm.Content{
		Role:  "user",
		Parts: []*domain_llm.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	if err := a.applyConfig(ctx); err != nil {
		return err
	}
	a.emit(events.StatusUpdate{Message: "Starting chat...", Level: "info"})
	return a.engine.Run(ctx, s.StartTime)
}

// GetCostTracker returns the session cost tracker used by the agent's engine.
func (a *agent) GetCostTracker() domain_pricing.ICostTracker {
	return a.engine.GetCostTracker()
}

// SetSystemInstructions updates the system instructions used by the context pipeline.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetSystemInstructions(ctx context.Context, instr string) error {
	a.mu.Lock()
	a.config.SystemInstructions = instr
	if a.gateway != nil {
		a.gateway.SetSystemInstructions(instr)
	}
	a.mu.Unlock()
	return a.applyConfig(ctx)
}

// WithSystemInstructions sets the initial system instructions.
func WithSystemInstructions(instr string) agentOption {
	return func(a *agent) {
		a.config.SystemInstructions = instr
	}
}

// WithSessionCostTracker sets the cost tracker for the agent.
func WithSessionCostTracker(tracker domain_pricing.ICostTracker) agentOption {
	return func(a *agent) {
		a.tracker = tracker
		if a.engine != nil {
			a.engine.ApplyOptions(withCostTracker(tracker))
		}
	}
}

// WithTraceLogger enables tracing and logs it to the specified file.
func WithTraceLogger(logFile string) agentOption {
	return func(a *agent) {
		telemetry.RegisterTraceSubscriber(a.events, logFile)
	}
}

// Shutdown gracefully stops the agent and its components.
func (a *agent) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.executor != nil {
		a.executor.Shutdown()
	}

	if a.events != nil {
		return a.events.Shutdown(ctx)
	}
	return nil
}
