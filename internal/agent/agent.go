// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"golang.org/x/sync/errgroup"
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
	gateway       domain_llm.LLMGateway
	engine        *orchestrator.Engine
	ctxManager    *session.ContextManager
	configWatcher session.ConfigWatcher
	strategy      *session.ContextStrategy
	executor      *executor.Dispatcher
	events        events.EventBus
	tracker       domain_pricing.CostTracker
	turnsLogger   ports.TurnsLogger
	logger        *slog.Logger

	config atomic.Pointer[runtimeConfig]
}

// NewAgent creates a new agent with required dependencies.
func NewAgent(client domain_llm.LLMGateway, bus events.EventBus, hManager ports.HistoryManager, providerName string, registry tools.Registry, sm domain_security.Manager, opts ...Option) (ports.Chatter, error) {
	cfg := &agentConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(registry))
	exec, err := executor.NewPipelineDispatcher(registry, sm, bus, telemetry.NewSlogLogger(cfg.logger), &executor.TelemetryLogger{})
	if err != nil {
		return nil, fmt.Errorf("failed to create tool executor: %w", err)
	}

	cw := session.NewNoOpConfigWatcher(domain_config.DefaultMaxHistoryTokens, domain_config.DefaultMaxToolTurns, domain_config.DefaultMaxHistoryTurns)

	if cfg.loader != nil || cfg.sessionLoader != nil {
		cw = session.NewFileConfigWatcher(cfg.loader, cfg.sessionLoader, domain_config.DefaultMaxHistoryTokens, domain_config.DefaultMaxToolTurns, domain_config.DefaultMaxHistoryTurns, cfg.logger)
	}

	a := &agent{
		gateway:       client,
		configWatcher: cw,
		strategy:      strategy,
		executor:      exec,
		events:        bus,
		tracker:       cfg.tracker,
		turnsLogger:   cfg.turnsLogger,
		logger:        cfg.logger,
	}

	a.config.Store(&runtimeConfig{
		ProviderName:     providerName,
		Model:            cfg.model,
		Mode:             cfg.mode,
		PricingOverrides: cfg.pricingOverrides,
		Limits: events.Limits{
			MaxHistoryTokens: domain_config.DefaultMaxHistoryTokens,
			MaxToolTurns:     domain_config.DefaultMaxToolTurns,
			MaxHistoryTurns:  domain_config.DefaultMaxHistoryTurns,
		},
	})

	factory := &session.PipelineFactory{
		Registry:      registry,
		History:       hManager,
		Summarizer:    cfg.summarizer,
		SkillSelector: cfg.skillSelector,
		Estimator:     strategy,
		Events:        bus,
	}

	ctxManager := session.NewContextManager(strategy, hManager, bus, factory,
		session.WithLogger(cfg.logger),
		session.WithSessionProvider(cfg.sessionProvider),
	)
	a.ctxManager = ctxManager

	// Initialize engine
	initialCfg := a.config.Load()
	a.engine = orchestrator.NewEngine(client, exec, ctxManager, registry, bus, strategy,
		orchestrator.WithEngineConfig(sm, initialCfg.ProviderName, initialCfg.Model, initialCfg.Mode, initialCfg.PricingOverrides),
		orchestrator.WithEngineCostTracker(a.tracker),
		orchestrator.WithEngineLogger(a.logger),
		orchestrator.WithEngineTurnsLogger(a.turnsLogger),
	)

	if cfg.registerInternal {
		if err := session.RegisterInternal(registry, ctxManager); err != nil {
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

	oldCfg := a.config.Load()
	a.configWatcher.Refresh(oldCfg.Model)

	tokens, toolTurns, histTurns, threshold := a.configWatcher.GetLimits()

	newCfg := *oldCfg // shallow copy
	newCfg.Limits.MaxHistoryTokens = tokens
	newCfg.Limits.MaxToolTurns = toolTurns
	newCfg.Limits.MaxHistoryTurns = histTurns
	newCfg.Limits.TieredThreshold = threshold

	if a.strategy != nil {
		a.configWatcher.SyncToStrategy(a.strategy)
	}

	a.config.Store(&newCfg)

	tracker := a.tracker

	if err := events.SafePublish(ctx, a.events, events.ConfigUpdated{Limits: newCfg.Limits}); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			a.getLogger().Error("event_publish_failed",
				slog.String("event_type", "ConfigUpdated"),
				slog.Any("error", err))
			return err
		}
	}

	if a.engine != nil {
		a.engine.Reconfigure(orchestrator.RuntimeConfig{
			ProviderName:     newCfg.ProviderName,
			Model:            newCfg.Model,
			Mode:             newCfg.Mode,
			PricingOverrides: newCfg.PricingOverrides,
		}, tracker)
	}
	if a.ctxManager != nil {
		a.ctxManager.Reconfigure(newCfg.Limits)
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

	// [REFACTOR] Use errgroup to coordinate the engine run loop and background telemetry workers.
	// We create a child context that is canceled when either the engine finishes or the background
	// tasks fail. We also use a defer cancel() to ensure the background tasks are stopped if engine finishes.
	gCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gCtx := errgroup.WithContext(gCtx)

	// Engine run loop (main orchestration)
	g.Go(func() error {
		defer cancel() // Stop other tasks if this one finishes
		return a.engine.Run(gCtx, s.StartTime)
	})

	// Background telemetry loop (coordinated via Listen)
	g.Go(func() error {
		return a.engine.StartTelemetry(gCtx)
	})

	return g.Wait()
}

// Shutdown gracefully stops the agent and its components.
func (a *agent) Shutdown(ctx context.Context) error {
	var errs []error

	if a.turnsLogger != nil {
		if err := a.turnsLogger.Close(); err != nil {
			a.getLogger().Debug("turns logger shutdown incomplete", slog.Any("error", err))
			errs = append(errs, err)
		}
	}

	if a.events != nil {
		if err := a.events.Flush(ctx); err != nil {
			a.getLogger().Debug("event bus flush incomplete during shutdown", slog.Any("error", err))
		}
		if err := a.events.Shutdown(ctx); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func (a *agent) getLogger() *slog.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}
