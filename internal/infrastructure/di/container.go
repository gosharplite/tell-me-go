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
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// ConfigurableSecurityManager extends the domain security manager with configuration methods.
type ConfigurableSecurityManager interface {
	security.Manager
	SetCommandsLogFile(path string)
	RegisterSafePath(path string)
	RegisterReadOnlyPath(path string)
	SetBypassActive(active bool)
	RegisterPolicyTools(r tools.Registry, kv ports.KVStore) error
}

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
		cfg.ClientFactory = infra_llm.NewClient
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
func (b *Bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(stdctx.Context) error, error) {
	b.cfg.Logger.Debug("Building session dependencies",
		slog.String("config_model", cfg.Model),
		slog.String("config_path", configPath),
		slog.Int("config_models_count", len(cfg.Models)))

	for k, v := range cfg.Models {
		b.cfg.Logger.Debug("Config model details",
			slog.String("model", k),
			slog.Float64("pricing_comp", v.Pricing.Comp))
	}

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

	bus := events.NewSimpleEventBus(ctx, events.WithLogger(b.cfg.Logger), events.WithAsync(false))

	pricingData, tracker, turnsLogger, cleanup := b.telemetryFactory.BuildTelemetry(ctx, paths, cfg, pricingOverrides, cleanup)

	lazyClient := newLazyClient(func() (llm.ExtendedClient, error) {
		return b.cfg.ClientFactory(cfg, pricingData, bus, telemetry.NewSlogLogger(b.cfg.Logger))
	})

	deps := &sessionDeps{
		paths:            paths,
		hManager:         hManager,
		sm:               b.cfg.SM,
		tracker:          tracker,
		pricingData:      pricingData,
		pricingOverrides: pricingOverrides,
		bus:              bus,
		logger:           telemetry.NewSlogLogger(b.cfg.Logger),
		turnsLogger:      turnsLogger,
		sessionProvider:  sessionProvider,
		workspacePolicy:  b.cfg.WorkspacePolicy,
		lazyClient:       lazyClient,
	}

	deps.health = b.healthFactory.BuildHealthManager(cfg, sessionProvider, lazyClient, b.toolchainFactory)

	lazyRegistry := newLazyRegistry(func() (tools.Registry, error) {
		return b.toolchainFactory.BuildRegistry(toolchainParams{
			Paths:            paths,
			SessionProvider:  sessionProvider,
			HealthManager:    deps.health,
			Client:           lazyClient,
			Bus:              bus,
			Model:            cfg.Model,
			Mode:             cfg.Mode,
			PricingOverrides: pricingOverrides,
			Capturer:         capturer,
		})
	}, telemetry.NewSlogLogger(b.cfg.Logger))
	deps.lazyRegistry = lazyRegistry

	return deps, hManager, cleanup, nil
}

type sessionDeps struct {
	paths            *persistence.Paths
	hManager         ports.HistoryManager
	sm               security.Manager
	tracker          pricing.CostTracker
	pricingData      pricing.PricingData
	pricingOverrides map[string]pricing.ModelPricing
	bus              events.EventBus
	logger           ports.Logger
	turnsLogger      ports.TurnsLogger
	sessionProvider  ports.SessionProvider
	workspacePolicy  services.WorkspacePolicy
	health           ports.HealthCheckManager

	lazyClient   *lazyClient
	lazyRegistry *lazyRegistry
}

func (d *sessionDeps) GetGateway() llm.LLMGateway {
	return d.lazyClient
}
func (d *sessionDeps) GetHistoryManager() ports.HistoryManager { return d.hManager }
func (d *sessionDeps) GetRegistry() (tools.Registry, error) {
	return d.lazyRegistry.get()
}
func (d *sessionDeps) GetSecurityManager() security.Manager { return d.sm }
func (d *sessionDeps) GetEventBus() events.EventBus         { return d.bus }
func (d *sessionDeps) GetLogger() ports.Logger              { return d.logger }
func (d *sessionDeps) GetTurnsLogger() ports.TurnsLogger    { return d.turnsLogger }
func (d *sessionDeps) GetPaths() *persistence.Paths         { return d.paths }
func (d *sessionDeps) GetSessionProvider() ports.SessionProvider {
	return d.sessionProvider
}
func (d *sessionDeps) GetWorkspacePolicy() services.WorkspacePolicy {
	return d.workspacePolicy
}
func (d *sessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing {
	return d.pricingOverrides
}
func (d *sessionDeps) GetTracker() pricing.CostTracker            { return d.tracker }
func (d *sessionDeps) GetPricingData() pricing.PricingData        { return d.pricingData }
func (d *sessionDeps) GetHealthManager() ports.HealthCheckManager { return d.health }
func (d *sessionDeps) GetClient() llm.LLMClient {
	return d.lazyClient
}

// GetAgentFactory returns a factory for creating Chatter instances.
func (b *Bootstrapper) GetAgentFactory() ports.ChatterFactory {
	return b.chatFactory.AgentFactory()
}

// FinalizeSession saves history and records session cost.
func (b *Bootstrapper) FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
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
