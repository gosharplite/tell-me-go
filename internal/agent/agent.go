// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// runtimeConfig consolidates all agent configuration parameters.
type runtimeConfig struct {
	Limits           events.Limits
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
}

// agent represents the chat orchestration logic (Stateless Service).
type agent struct {
	mu            sync.RWMutex
	gateway       domain_llm.LLMGateway
	engine        *turnEngine
	ctxManager    *orchestration.ContextManager
	configWatcher orchestration.ConfigWatcher
	strategy      *orchestration.ContextStrategy
	executor      *executor.ToolExecutor
	events        events.EventBus
	tracker       domain_pricing.CostTracker
	logger        *slog.Logger

	config runtimeConfig
}

// NewAgent creates a new agent with required dependencies.
func NewAgent(client domain_llm.LLMGateway, bus events.EventBus, hManager ports.HistoryManager, providerName string, registry tools.Registry, sm domain_security.Manager, opts ...Option) (ports.Chatter, error) {
	cfg := &agentConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(registry))
	exec, err := executor.NewToolExecutor(registry, sm, bus, telemetry.NewSlogLogger(cfg.logger), &executor.TelemetryLogger{})
	if err != nil {
		return nil, fmt.Errorf("failed to create tool executor: %w", err)
	}

	cw := orchestration.NewNoOpConfigWatcher(domain_config.DefaultMaxHistoryTokens, domain_config.DefaultMaxToolTurns, domain_config.DefaultMaxHistoryTurns)

	if cfg.loader != nil || cfg.sessionLoader != nil {
		cw = orchestration.NewFileConfigWatcher(cfg.loader, cfg.sessionLoader, domain_config.DefaultMaxHistoryTokens, domain_config.DefaultMaxToolTurns, domain_config.DefaultMaxHistoryTurns, cfg.logger)
	}

	a := &agent{
		gateway:       client,
		configWatcher: cw,
		strategy:      strategy,
		executor:      exec,
		events:        bus,
		tracker:       cfg.tracker,
		logger:        cfg.logger,
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
		Registry:      registry,
		History:       hManager,
		Summarizer:    cfg.summarizer,
		SkillSelector: cfg.skillSelector,
		Estimator:     strategy,
		Events:        bus,
	}

	ctxManager := orchestration.NewContextManager(strategy, hManager, bus, factory, orchestration.WithLogger(cfg.logger))
	a.ctxManager = ctxManager

	// Initialize engine
	a.engine = newTurnEngine(client, exec, ctxManager, registry, bus, strategy,
		withEngineConfig(sm, a.config.ProviderName, a.config.Model, a.config.Mode, a.config.PricingOverrides),
		withEngineCostTracker(a.tracker),
		withEngineLogger(a.logger),
	)

	if cfg.registerInternal {
		if err := orchestration.RegisterInternal(registry, ctxManager); err != nil {
			return nil, fmt.Errorf("failed to register internal tools: %w", err)
		}
	}

	initCtx := cfg.initCtx
	if initCtx == nil {
		initCtx = context.Background()
	}

	if err := a.applyConfig(initCtx); err != nil {
		a.emit(context.Background(), events.StatusUpdate{Message: "failed to apply initial configuration", Level: "warning"})
	}
	return a, nil
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

	if err := events.SafePublish(ctx, a.events, events.ConfigUpdated{Limits: cfg.Limits}); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			a.getLogger().Error("event_publish_failed",
				slog.String("event_type", "ConfigUpdated"),
				slog.Any("error", err))
			return err
		}
	}

	if a.engine != nil {
		a.engine.Reconfigure(cfg, tracker)
	}
	if a.ctxManager != nil {
		a.ctxManager.Reconfigure(cfg.Limits)
	}
	return nil
}

func (a *agent) Subscribe(sub func(context.Context, events.Event)) {
	a.events.Subscribe(sub)
}

func (a *agent) emit(ctx context.Context, e events.Event) {
	// [SCALABILITY FIX] Always use a bounded context for publishing events
	// to prevent cascading system deadlocks if a subscriber stalls.
	if err := events.SafePublish(ctx, a.events, e); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			a.getLogger().Error("event_publish_failed",
				slog.String("event_type", string(e.Type())),
				slog.Any("error", err))
		}
	}
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
func (a *agent) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	if err := a.ctxManager.AddContent(ctx, &domain_llm.Content{
		Role:  "user",
		Parts: []*domain_llm.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	if err := a.applyConfig(ctx); err != nil {
		return err
	}
	a.emit(ctx, events.StatusUpdate{Message: "Starting chat...", Level: "info"})
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
		if err := a.events.Flush(ctx); err != nil {
			a.getLogger().Debug("event bus flush incomplete during shutdown", slog.Any("error", err))
		}
		err := a.events.Shutdown(ctx)
		if errors.Is(err, events.ErrBusNotInitialized) {
			return nil
		}
		return err
	}
	return nil
}

func (a *agent) getLogger() *slog.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}
