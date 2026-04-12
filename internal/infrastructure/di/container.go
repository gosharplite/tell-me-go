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

	"github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/application/suggestions"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui"
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
	sessionFactory   sessionFactory
	toolchainFactory toolchainFactory
	telemetryFactory telemetryFactory
	HomeDir          string
	SM               ConfigurableSecurityManager
	Version          string
	Stdout           io.Writer
	Stderr           io.Writer
	Logger           *slog.Logger
	FileSystem       infra_persistence.FileSystem
	ClientFactory    func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)
	RegisterAllTools func(params infra_tools.ToolRegistrationParams) error
	RegisterMetrics  func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error
	RotateSession    func(ctx stdctx.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error
	NewSessionState  func(ctx stdctx.Context, modeDir string) (ports.SessionProvider, error)
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

	b.sessionFactory = newSessionFactory(homeDir, fs, sm, stdout, stderr, logger,
		func(ctx stdctx.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error {
			return b.RotateSession(ctx, fs, stdout, paths, retentionDays)
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
	return b
}

// BuildSessionDependencies assembles all dependencies required for a chat session.
func (b *Bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(stdctx.Context) error, error) {
	pricingOverrides := b.getPricingOverrides(cfg)
	sessionProvider, paths, cleanup, err := b.sessionFactory.BuildSession(ctx, cfg, configPath, newSession, pricingOverrides)
	if err != nil {
		return nil, nil, nil, err
	}

	hManager, err := b.buildHistoryManager(ctx, paths)
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, nil, err
	}

	bus := events.NewSimpleEventBus(ctx, events.WithLogger(b.Logger), events.WithAsync(false))

	pricingData, tracker, turnsLogger, cleanup := b.telemetryFactory.BuildTelemetry(ctx, paths, cfg, cleanup)

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
		clientFactory:    clientFactory,
	}

	regFactory := func() (tools.Registry, error) {
		return b.toolchainFactory.BuildRegistry(toolchainParams{
			Paths:            paths,
			SessionProvider:  sessionProvider,
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

func (b *Bootstrapper) buildHistoryManager(ctx stdctx.Context, paths *persistence.Paths) (*history.Manager, error) {
	hManager := history.NewManager(infra_persistence.NewDomainFS(b.FileSystem), paths.HistoryPath, paths.HistoryArchivePath)
	if err := hManager.Load(ctx); err != nil {
		if !errors.Is(err, ports.ErrHistoryNotFound) {
			return nil, fmt.Errorf("%w: failed to load history from %s: %w", errInfraInit, paths.HistoryPath, err)
		}
	}
	return hManager, nil
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
func (d *sessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing {
	return d.pricingOverrides
}
func (d *sessionDeps) GetTracker() pricing.CostTracker     { return d.tracker }
func (d *sessionDeps) GetPricingData() pricing.PricingData { return d.pricingData }
func (d *sessionDeps) GetClient() llm.LLMClient {
	return &lazyLLMProxy{deps: d}
}

// GetAgentFactory returns a factory for creating Chatter instances.
func (b *Bootstrapper) GetAgentFactory() ports.ChatterFactory {
	return factory.NewChatter
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
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (b *Bootstrapper) GetHistoryManager(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
	paths := persistence.ResolvePaths(b.HomeDir, cfg.Mode)
	if err := infra_persistence.EnsureDirectories(ctx, b.FileSystem, paths); err != nil {
		return nil, fmt.Errorf("%w: failed to ensure session directories for %s: %w", errInfraInit, cfg.Mode, err)
	}

	hManager, err := b.buildHistoryManager(ctx, paths)
	if err != nil {
		return nil, err // buildHistoryManager already wraps it
	}

	return hManager, nil
}

// GetUnifiedHistoryProvider assembles the read-model for the history browser.
func (b *Bootstrapper) GetUnifiedHistoryProvider(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	paths := persistence.ResolvePaths(b.HomeDir, cfg.Mode)
	if err := infra_persistence.EnsureDirectories(ctx, b.FileSystem, paths); err != nil {
		return nil, fmt.Errorf("%w: failed to ensure session directories for unified history: %w", errInfraInit, err)
	}

	archiveReader := history.NewJSONLArchiveReader(infra_persistence.NewDomainFS(b.FileSystem), paths.HistoryArchivePath)

	return history.NewUnifiedProvider(archiveReader, hManager), nil
}

// GetSuggestionService initializes and returns the suggestion service.
func (b *Bootstrapper) GetSuggestionService(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
	tracker, err := history.NewGlobalPromptTracker(b.HomeDir)
	if err != nil {
		b.Logger.Warn("failed to initialize global prompt tracker, falling back to no-op", "error", err)
		tracker = history.NewNoOpTracker()
	}

	return suggestions.NewMultiSourceSuggestionService(ctx, infra_persistence.NewDomainFS(b.FileSystem), tracker, recentHistory, b.Stderr)
}

// getSystemMetricsProvider returns the system metrics provider based on the platform.
func (b *Bootstrapper) getSystemMetricsProvider() ports.SystemMetricsProvider {
	return telemetry.NewSystemMetricsProvider()
}

// GetUIRenderer returns a UI renderer configured with the bootstrapper's output writers.
func (b *Bootstrapper) GetUIRenderer() ports.UIRenderer {
	return ui.NewRenderer(b.SM, b.Stdout, b.Stderr, clock.RealClock{}, b.getSystemMetricsProvider())
}

// GetHistoryRenderer returns a history renderer.
func (b *Bootstrapper) GetHistoryRenderer() ports.HistoryRenderer {
	return &ui.StdHistoryRenderer{}
}

// tuiHistoryBrowser implements ports.HistoryBrowser using the TUI.
type tuiHistoryBrowser struct {
	stdout io.Writer
	stderr io.Writer
	logger *slog.Logger
}

// Browse launches the TUI history browser.
func (b *tuiHistoryBrowser) Browse(ctx stdctx.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	if closer, err := tui.InitLogger(); err == nil {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				b.logger.Warn("failed to close tui logger", "error", closeErr)
			}
		}()
	}

	model := tui.NewRootBrowserModel(ctx, provider, hManager)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui program error: %w", err)
	}
	return nil
}

// GetHistoryBrowser returns a history browser that launches the TUI.
func (b *Bootstrapper) GetHistoryBrowser() ports.HistoryBrowser {
	return &tuiHistoryBrowser{
		stdout: b.Stdout,
		stderr: b.Stderr,
		logger: b.Logger,
	}
}

// GetChatService returns a chat service instance.
func (b *Bootstrapper) GetChatService() agent.ChatService {
	return agent.NewChatService(
		b.HomeDir,
		b.Version,
		b.Stdout,
		b.Stderr,
		b.SM,
		b,
		b.GetAgentFactory(),
		b.GetUIRenderer(),
		b.GetHistoryRenderer(),
		b.GetHistoryBrowser(),
		infra_persistence.NewDomainFS(b.FileSystem),
	)
}
