// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// Bootstrapper handles the instantiation and wiring of system components.
type Bootstrapper struct {
	cfg               BootstrapperConfig
	sessionFactory    sessionFactory
	toolchainFactory  toolchainFactory
	telemetryFactory  telemetryFactory
	historyFactory    historyFactory
	healthFactory     healthFactory
	uiFactory         uiFactory
	chatFactory       chatFactory
	suggestionFactory suggestionFactory
}

// NewBootstrapper creates a new Bootstrapper instance.
func NewBootstrapper(cfg BootstrapperConfig) *Bootstrapper {
	if cfg.ClientFactory == nil {
		cfg.ClientFactory = &infra_llm.DefaultClientFactory{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.FileSystem == nil {
		cfg.FileSystem = &infra_persistence.OSFileSystem{}
	}

	b := &Bootstrapper{cfg: cfg}

	if b.cfg.WorkspacePolicy == nil {
		b.cfg.WorkspacePolicy = infra_persistence.NewWorkspacePolicy()
	}

	b.sessionFactory = newSessionFactory(
		cfg.HomeDir, cfg.FileSystem, cfg.SM, cfg.Stdout, cfg.Stderr, cfg.Logger,
		cfg.RotateSession, cfg.NewSessionState,
	)
	b.toolchainFactory = newToolchainFactory(
		cfg.HomeDir, cfg.FileSystem, cfg.SM, b.cfg.WorkspacePolicy,
		cfg.RegisterAllTools, cfg.RegisterMetrics,
	)
	b.telemetryFactory = newTelemetryFactory(cfg.HomeDir, cfg.FileSystem, cfg.SM, cfg.Logger)
	b.historyFactory = newHistoryFactory(cfg.HomeDir, cfg.FileSystem)
	b.healthFactory = newHealthFactory()
	b.uiFactory = newUIFactory(cfg.SM, cfg.Stdout, cfg.Stderr, cfg.Logger)
	b.chatFactory = newChatFactory(cfg.HomeDir, cfg.Version, cfg.Stdout, cfg.Stderr, cfg.SM, cfg.FileSystem, b, b.uiFactory)
	b.suggestionFactory = newSuggestionFactory(cfg.HomeDir, cfg.FileSystem, cfg.Stderr, cfg.Logger, b.cfg.WorkspacePolicy)
	return b
}

// BuildSessionDependencies assembles all dependencies required for a chat session.
func (b *Bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(stdctx.Context) error, error) {
	b.logBuildStart(cfg, configPath)

	pricingOverrides := b.getPricingOverrides(cfg)
	sessionProvider, paths, cleanup, err := b.sessionFactory.BuildSession(ctx, cfg, configPath, newSession, pricingOverrides)
	if err != nil {
		return nil, nil, nil, err
	}

	hManager, err := b.historyFactory.BuildHistoryManager(ctx, cfg)
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, nil, err
	}

	bus, logger := b.wireInfrastructure(ctx)
	pricingData, tracker, turnsLogger, cleanup := b.wireTelemetry(ctx, paths, cfg, pricingOverrides, cleanup)
	lazyClient := b.wireLLMClient(cfg, pricingData, bus, logger)

	deps := &sessionDeps{
		infraProvider: infraProvider{
			paths: paths, sm: b.cfg.SM, bus: bus, logger: logger, turnsLogger: turnsLogger,
		},
		telemetryProvider:    telemetryProvider{tracker: tracker, pricingOverrides: pricingOverrides},
		sessionStateProvider: sessionStateProvider{hManager: hManager, sessionProvider: sessionProvider, workspacePolicy: b.cfg.WorkspacePolicy},
		lazyProvider:         lazyProvider{client: lazyClient},
	}

	deps.health = b.wireHealth(cfg, sessionProvider, lazyClient)
	deps.registry = b.wireToolRegistry(paths, sessionProvider, deps.health, lazyClient, bus, cfg, pricingOverrides, capturer)

	return deps, hManager, cleanup, nil
}

// logBuildStart emits debug-level diagnostics about the configuration being used.
func (b *Bootstrapper) logBuildStart(cfg *config.Config, configPath string) {
	b.cfg.Logger.Debug("Building session dependencies",
		slog.String("config_model", cfg.Model),
		slog.String("config_path", configPath),
		slog.Int("config_models_count", len(cfg.Models)))

	for k, v := range cfg.Models {
		b.cfg.Logger.Debug("Config model details",
			slog.String("model", k),
			slog.Float64("pricing_comp", v.Pricing.Comp))
	}
}

// wireInfrastructure creates the event bus and slog-backed logger.
func (b *Bootstrapper) wireInfrastructure(ctx stdctx.Context) (events.EventBus, ports.Logger) {
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(b.cfg.Logger), events.WithAsync(false))
	logger := telemetry.NewSlogLogger(b.cfg.Logger)
	return bus, logger
}

// wireTelemetry delegates to the telemetryFactory to build pricing data,
// cost tracking, and turns logging.
func (b *Bootstrapper) wireTelemetry(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing, cleanup func(stdctx.Context) error) (pricing.PricingData, pricing.CostTracker, ports.TurnsLogger, func(stdctx.Context) error) {
	return b.telemetryFactory.BuildTelemetry(ctx, paths, cfg, pricingOverrides, cleanup)
}

// wireLLMClient creates a lazily-initialized LLM client. The underlying
// provider client is not constructed until the first Generate/SendChat call.
func (b *Bootstrapper) wireLLMClient(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) *lazyClient {
	return newLazyClient(func() (llm.ExtendedClient, error) {
		if len(cfg.FailoverOrder) > 0 {
			gw, err := b.cfg.ClientFactory.NewFailoverChain(cfg, pricingData, bus, logger)
			if err != nil {
				return nil, err
			}
			if gw != nil {
				return gw, nil
			}
		}
		return b.cfg.ClientFactory.NewClient(cfg, pricingData, bus, logger)
	})
}

// wireHealth builds the health check manager wired with persistence,
// LLM provider, and toolchain health checkers.
func (b *Bootstrapper) wireHealth(cfg *config.Config, sessionProvider ports.SessionProvider, lazyClient llm.ExtendedClient) ports.HealthCheckManager {
	return b.healthFactory.BuildHealthManager(cfg, sessionProvider, lazyClient, b.toolchainFactory)
}

// wireToolRegistry creates a lazily-initialized tool registry. Tool
// registration (filesystem scanning, binary discovery, security policy
// evaluation) is deferred until the first call to GetRegistry.
func (b *Bootstrapper) wireToolRegistry(paths *persistence.Paths, sessionProvider ports.SessionProvider, health ports.HealthCheckManager, lazyClient *lazyClient, bus events.EventBus, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing, capturer agent.CapturerInteractor) *lazyRegistry {
	return newLazyRegistry(func() (tools.Registry, error) {
		return b.toolchainFactory.BuildRegistry(toolchainParams{
			Paths:            paths,
			SessionProvider:  sessionProvider,
			HealthManager:    health,
			Client:           lazyClient,
			Bus:              bus,
			Model:            cfg.Model,
			Mode:             cfg.Mode,
			PricingOverrides: pricingOverrides,
			Capturer:         capturer,
		})
	}, telemetry.NewSlogLogger(b.cfg.Logger))
}

type sessionDeps struct {
	infraProvider
	telemetryProvider
	sessionStateProvider
	lazyProvider
	healthProvider
}

// GetAgentFactory returns a factory for creating Chatter instances.
func (b *Bootstrapper) GetAgentFactory() ports.ChatterFactory {
	return b.chatFactory.AgentFactory()
}

// FinalizeSession saves history and records session cost.
func (b *Bootstrapper) FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, cfg *config.Config) error {
	var errs []error

	if saveErr := hManager.Save(ctx); saveErr != nil {
		errs = append(errs, fmt.Errorf("error saving history: %w", saveErr))
	}

	// Calculate and record cost
	if recordErr := telemetry.RecordSessionCost(ctx, b.cfg.SM, deps.GetTracker(), deps.GetPaths().LogPath, cfg.Model, cfg.Mode, "", deps.GetPricingOverrides()); recordErr != nil {
		errs = append(errs, fmt.Errorf("failed to record final session cost: %w", recordErr))
	}

	return errors.Join(errs...)
}

// getPricingOverrides extracts pricing overrides from the configuration.
func (b *Bootstrapper) getPricingOverrides(cfg *config.Config) map[string]pricing.ModelPricing {
	pricingOverrides := make(map[string]pricing.ModelPricing)

	for k, v := range cfg.Models {
		// Now that dots are supported, 'k' will correctly be "deepseek-ai/deepseek-v3.2-maas"
		// and 'v.Pricing' will be fully populated from the YAML.
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
			b.cfg.Logger.Debug("Added pricing override for model",
				slog.String("model", k),
				slog.Float64("comp", v.Pricing.Comp))
		}
	}
	return pricingOverrides
}

func (b *Bootstrapper) GetHistoryManager(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
	return b.historyFactory.BuildHistoryManager(ctx, cfg)
}

// GetUnifiedHistoryProvider assembles the read-model for the history browser.
func (b *Bootstrapper) GetUnifiedHistoryProvider(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	return b.historyFactory.BuildUnifiedHistoryProvider(ctx, cfg, hManager)
}

// GetSuggestionService initializes and returns the suggestion service.
func (b *Bootstrapper) GetSuggestionService(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
	return b.suggestionFactory.BuildSuggestionService(ctx, recentHistory)
}

// GetHistoryBrowser returns a history browser that launches the TUI.
func (b *Bootstrapper) GetHistoryBrowser() ports.HistoryBrowser {
	return b.uiFactory.HistoryBrowser()
}

// GetUIRenderer returns a UI renderer configured with the bootstrapper's output writers.
func (b *Bootstrapper) GetUIRenderer() ports.UIRenderer {
	return b.uiFactory.UIRenderer()
}

// GetHistoryRenderer returns a history renderer.
func (b *Bootstrapper) GetHistoryRenderer() ports.HistoryRenderer {
	return b.uiFactory.HistoryRenderer()
}

// GetChatService returns a chat service instance.
func (b *Bootstrapper) GetChatService() agent.ChatService {
	return b.chatFactory.ChatService()
}
