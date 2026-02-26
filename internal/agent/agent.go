// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Registry defines the tool management interface needed by the agent.
type Registry interface {
	tools.IToolRegistry
}

// SecurityManager defines the safety controls needed for tool execution.
type SecurityManager interface {
	domain_security.ISecurityManager
}

// runtimeConfig consolidates all agent configuration parameters.
type runtimeConfig struct {
	Limits           events.Limits
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
}

// Agent represents the chat orchestration logic (Stateless Service).
type agent struct {
	mu            sync.RWMutex
	gateway       domain_llm.LLMGateway
	engine        *turnEngine
	ctxManager    *orchestration.ContextManager
	configWatcher *orchestration.ConfigWatcher
	strategy      *orchestration.ContextStrategy
	executor      *executor.ToolExecutor
	events        events.EventBus
	tracker       domain_pricing.ICostTracker

	config runtimeConfig
}

// New creates a new Agent with required dependencies.
func New(client domain_llm.LLMGateway, bus events.EventBus, providerName string, registry Registry, sm SecurityManager, opts ...Option) *agent {
	cfg := &agentConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(registry), bus)
	exec := executor.NewToolExecutor(registry, sm, bus)

	a := &agent{
		gateway:       client,
		configWatcher: orchestration.NewConfigWatcher(cfg.loader, domain_config.DefaultMaxHistoryTokens, domain_config.DefaultMaxToolTurns, domain_config.DefaultMaxHistoryTurns),
		strategy:      strategy,
		executor:      exec,
		events:        bus,
		tracker:       cfg.tracker,
		config: runtimeConfig{
			ProviderName:     providerName,
			Model:            cfg.model,
			Mode:             cfg.mode,
			PricingOverrides: cfg.pricingOverrides,
			Limits: events.Limits{
				MaxHistoryTokens: domain_config.DefaultMaxHistoryTokens,
				MaxToolTurns:     domain_config.DefaultMaxToolTurns,
				MaxHistoryTurns:  domain_config.DefaultMaxHistoryTurns,
			},
		},
	}

	factory := &orchestration.PipelineFactory{
		Registry:   registry,
		History:    cfg.hManager,
		Summarizer: cfg.summarizer,
		Estimator:  strategy,
		Events:     bus,
	}

	ctxManager := orchestration.NewContextManager(strategy, cfg.hManager, bus, factory)
	a.ctxManager = ctxManager

	// Initialize engine
	a.engine = newTurnEngine(client, exec, ctxManager, registry, bus,
		withConfig(sm, a.config.ProviderName, a.config.Model, a.config.PricingOverrides),
		withCostTracker(a.tracker),
	)

	if cfg.registerInternal {
		orchestration.RegisterInternal(registry, ctxManager)
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

// SetTieredThreshold sets the tiered threshold for the agent.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetTieredThreshold(ctx context.Context, threshold int) error {
	a.configWatcher.ApplyLimits(events.Limits{TieredThreshold: threshold})
	return a.applyConfig(ctx)
}

// Chat runs the multi-turn orchestration loop.
func (a *agent) Chat(ctx context.Context, s *services.Session, prompt string) error {
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
