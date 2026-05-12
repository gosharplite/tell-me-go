// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	infra_toolchain "github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
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
	sessionFactory    sessionFactory
	toolchainFactory  toolchainFactory
	telemetryFactory  telemetryFactory
	historyFactory    historyFactory
	uiFactory         uiFactory
	chatFactory       chatFactory
	suggestionFactory suggestionFactory
	HomeDir           string
	SM                ConfigurableSecurityManager
	Version           string
	Stdout            io.Writer
	Stderr            io.Writer
	Logger            *slog.Logger
	FileSystem        infra_persistence.FileSystem
	WorkspacePolicy   services.WorkspacePolicy
	ClientFactory     func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)
	RegisterAllTools  func(params infra_tools.ToolRegistrationParams) error
	RegisterMetrics   func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error
	RotateSession     func(ctx stdctx.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int, logger *slog.Logger) error
	NewSessionState   func(ctx stdctx.Context, modeDir string) (ports.SessionProvider, error)
}

// NewBootstrapper creates a new Bootstrapper instance.
func NewBootstrapper(homeDir string, sm ConfigurableSecurityManager, version string, stdout, stderr io.Writer, logger *slog.Logger, fs infra_persistence.FileSystem, clientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)) *Bootstrapper {
	if clientFactory == nil {
		clientFactory = infra_llm.NewClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	if fs == nil {
		fs = &infra_persistence.OSFileSystem{}
	}
	b := &Bootstrapper{
		HomeDir:          homeDir,
		SM:               sm,
		Version:          version,
		Stdout:           stdout,
		Stderr:           stderr,
		Logger:           logger,
		FileSystem:       fs,
		ClientFactory:    clientFactory,
		RegisterAllTools: infra_tools.RegisterAll,
		RegisterMetrics:  telemetry.RegisterMetrics,
		RotateSession:    infra_persistence.RotateSession,
		NewSessionState:  infra_persistence.NewSessionState,
	}

	if b.WorkspacePolicy == nil {
		b.WorkspacePolicy = infra_persistence.NewWorkspacePolicy()
	}

	b.sessionFactory = newSessionFactory(homeDir, fs, sm, stdout, stderr, logger,
		func(ctx stdctx.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int, logger *slog.Logger) error {
			return b.RotateSession(ctx, fs, stdout, paths, retentionDays, logger)
		},
		func(ctx stdctx.Context, modeDir string) (ports.SessionProvider, error) {
			return b.NewSessionState(ctx, modeDir)
		})
	b.toolchainFactory = newToolchainFactory(homeDir, fs, sm,
		func(params infra_tools.ToolRegistrationParams) error {
			return b.RegisterAllTools(params)
		},
		func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
			return b.RegisterMetrics(r, sm, logFile, traceFile, model, mode, pricingOverrides, kvStore)
		})
	b.telemetryFactory = newTelemetryFactory(homeDir, fs, sm, logger)
	b.historyFactory = newHistoryFactory(homeDir, fs)
	b.uiFactory = newUIFactory(sm, stdout, stderr, logger)
	b.chatFactory = newChatFactory(homeDir, version, stdout, stderr, sm, fs, b, b.uiFactory)
	b.suggestionFactory = newSuggestionFactory(homeDir, fs, stderr, logger, b.WorkspacePolicy)
	return b
}

// BuildSessionDependencies assembles all dependencies required for a chat session.
func (b *Bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(stdctx.Context) error, error) {
	b.Logger.Debug("Building session dependencies",
		slog.String("config_model", cfg.Model),
		slog.String("config_path", configPath),
		slog.Int("config_models_count", len(cfg.Models)))

	for k, v := range cfg.Models {
		b.Logger.Debug("Config model details",
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

	bus := events.NewSimpleEventBus(ctx, events.WithLogger(b.Logger), events.WithAsync(false))

	pricingData, tracker, turnsLogger, cleanup := b.telemetryFactory.BuildTelemetry(ctx, paths, cfg, pricingOverrides, cleanup)

	// Lazy initialization factory for the LLM client.
	clientFactory := func() (llm.ExtendedClient, error) {
		return b.ClientFactory(cfg, pricingData, bus, telemetry.NewSlogLogger(b.Logger))
	}

	deps := &sessionDeps{
		paths:            paths,
		hManager:         hManager,
		sm:               b.SM,
		tracker:          tracker,
		pricingData:      pricingData,
		pricingOverrides: pricingOverrides,
		bus:              bus,
		logger:           telemetry.NewSlogLogger(b.Logger),
		turnsLogger:      turnsLogger,
		sessionProvider:  sessionProvider,
		workspacePolicy:  b.WorkspacePolicy,
		clientFactory:    clientFactory,
	}

	// Initialize Health Manager
	p := cfg.GetActiveProvider()
	authenticator, _ := infra_llm.CreateAuthenticator(&p) // Error is handled in health check itself
	healthCheckers := map[ports.Component]ports.HealthChecker{
		ports.CompPersistence: sessionProvider.GetHealthChecker(),
		ports.CompLLMProvider: infra_llm.NewLLMProviderHealthChecker(p.Type, authenticator, p.URL, &lazyLLMProxy{deps: deps}),
		ports.CompToolchain:   infra_toolchain.NewToolchainHealthChecker(&exec.RealExecutor{}, []string{"git", "go"}, []string{"make"}),
	}
	deps.health = factory.NewHealthCheckManager(healthCheckers)

	regFactory := func() (tools.Registry, error) {
		return b.toolchainFactory.BuildRegistry(toolchainParams{
			Paths:            paths,
			SessionProvider:  sessionProvider,
			HealthManager:    deps.health,
			Client:           &lazyLLMProxy{deps: deps},
			Bus:              bus,
			Model:            cfg.Model,
			Mode:             cfg.Mode,
			PricingOverrides: pricingOverrides,
			Capturer:         capturer,
		})
	}
	deps.regFactory = regFactory

	return deps, hManager, cleanup, nil
}

type sessionDeps struct {
	paths            *persistence.Paths
	hManager         ports.HistoryManager
	client           llm.LLMClient
	gw               llm.LLMGateway
	reg              tools.Registry
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

	initOnce      sync.Once
	clientErr     error
	clientFactory func() (llm.ExtendedClient, error)

	regOnce    sync.Once
	regErr     error
	regFactory func() (tools.Registry, error)
}

type lazyLLMProxy struct {
	deps *sessionDeps
}

func (p *lazyLLMProxy) Generate(ctx stdctx.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	p.deps.initClient()
	if p.deps.clientErr != nil {
		return nil, nil, fmt.Errorf("LLM provider initialization failed: %w", p.deps.clientErr)
	}
	return p.deps.gw.Generate(ctx, input, tools, resolver)
}

func (p *lazyLLMProxy) SendChat(ctx stdctx.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	p.deps.initClient()
	if p.deps.clientErr != nil {
		return nil, nil, fmt.Errorf("LLM provider initialization failed: %w", p.deps.clientErr)
	}
	return p.deps.client.SendChat(ctx, history, tools, resolver)
}

func (p *lazyLLMProxy) GenerateImages(ctx stdctx.Context, model, prompt string, mimeType string) ([][]byte, error) {
	p.deps.initClient()
	if p.deps.clientErr != nil {
		return nil, fmt.Errorf("LLM provider initialization failed: %w", p.deps.clientErr)
	}
	return p.deps.client.GenerateImages(ctx, model, prompt, mimeType)
}

func (p *lazyLLMProxy) RefreshAuth() error {
	p.deps.initClient()
	if p.deps.clientErr != nil {
		return fmt.Errorf("LLM provider initialization failed: %w", p.deps.clientErr)
	}
	return p.deps.client.RefreshAuth()
}

func (d *sessionDeps) initClient() {
	d.initOnce.Do(func() {
		client, err := d.clientFactory()
		if err != nil {
			d.clientErr = err
			return
		}
		d.client = client
		d.gw = client
	})
}

func (d *sessionDeps) GetGateway() llm.LLMGateway {
	return &lazyLLMProxy{deps: d}
}
func (d *sessionDeps) GetHistoryManager() ports.HistoryManager { return d.hManager }
func (d *sessionDeps) GetRegistry() (tools.Registry, error) {
	d.regOnce.Do(func() {
		reg, err := d.regFactory()
		if err != nil {
			d.regErr = err
			d.logger.Error("failed to lazily initialize tool registry", slog.Any("error", err))
			return
		}
		d.reg = reg
	})
	return d.reg, d.regErr
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
	return &lazyLLMProxy{deps: d}
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
	if recordErr := telemetry.RecordSessionCost(ctx, b.SM, deps.GetTracker(), deps.GetPaths().LogPath, cfg.Model, cfg.Mode, "", deps.GetPricingOverrides()); recordErr != nil {
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
			b.Logger.Debug("Added pricing override for model",
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
