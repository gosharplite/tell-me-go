// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"time"

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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/logging"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
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
	HomeDir          string
	SM               ConfigurableSecurityManager
	Version          string
	Stdout           io.Writer
	Stderr           io.Writer
	Logger           *slog.Logger
	ClientFactory    func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)
	RegisterAllTools func(params infra_tools.ToolRegistrationParams) error
	RegisterMetrics  func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error
	RotateSession    func(fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error
	NewSessionState  func(ctx stdctx.Context, modeDir string) (ports.SessionProvider, error)
}

// NewBootstrapper creates a new Bootstrapper instance.
func NewBootstrapper(homeDir string, sm ConfigurableSecurityManager, version string, stdout, stderr io.Writer, logger *slog.Logger, clientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)) *Bootstrapper {
	if clientFactory == nil {
		clientFactory = infra_llm.NewClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Bootstrapper{
		HomeDir:          homeDir,
		SM:               sm,
		Version:          version,
		Stdout:           stdout,
		Stderr:           stderr,
		Logger:           logger,
		ClientFactory:    clientFactory,
		RegisterAllTools: infra_tools.RegisterAll,
		RegisterMetrics:  telemetry.RegisterMetrics,
		RotateSession:    infra_persistence.RotateSession,
		NewSessionState:  infra_persistence.NewSessionState,
	}
}

// BuildSessionDependencies assembles all dependencies required for a chat session.
func (b *Bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(stdctx.Context) error, error) {
	paths, err := infra_persistence.InitializePaths(&infra_persistence.OSFileSystem{}, b.HomeDir, cfg.Mode)
	if err != nil {
		return nil, nil, nil, err
	}

	pricingOverrides := b.getPricingOverrides(cfg)
	if err := b.setupSecurity(paths, configPath); err != nil {
		return nil, nil, nil, err
	}

	sessionProvider, cleanup, err := b.buildSessionProvider(ctx, paths, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	b.applySessionSecuritySettings(ctx, sessionProvider)

	if newSession {
		// Hard dependency: session rotation must complete before we continue.
		// Errors here MUST halt execution to prevent state corruption.
		if err := b.handleNewSession(ctx, paths, cfg, pricingOverrides, sessionProvider.GetSettings()); err != nil {
			_ = cleanup(ctx)
			return nil, nil, nil, fmt.Errorf("session initialization failed during rotation: %w", err)
		}
	}

	// Initialize commands log after session rotation to avoid file locks on Windows.
	b.SM.SetCommandsLogFile(paths.CommandsLogPath)

	hManager, err := b.buildHistoryManager(ctx, paths)
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, nil, err
	}

	bus := events.NewSimpleEventBus(ctx, events.WithLogger(b.Logger), events.WithAsync(false))

	pricingData := telemetry.GetPricing(ctx, b.SM, filepath.Join(b.HomeDir, "output"))

	client, err := b.ClientFactory(cfg, pricingData, bus, telemetry.NewSlogLogger(b.Logger))
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, nil, fmt.Errorf("error creating client: %w", err)
	}

	reg, err := b.buildToolRegistry(infra_tools.ToolRegistrationParams{
		SecurityManager:  b.SM,
		CommandExecutor:  &exec.RealExecutor{},
		CommandValidator: internal_security.NewCommandValidator(b.SM, capturer),
		SessionProvider:  sessionProvider,
		LogFile:          paths.LogPath,
		TraceFile:        paths.TracePath,
		Model:            cfg.Model,
		Mode:             cfg.Mode,
		PricingOverrides: pricingOverrides,
		Client:           client,
		AssetsDir:        filepath.Join(b.HomeDir, "assets/generated"),
		EventBus:         bus,
		FileSystem:       infra_persistence.NewOSFileSystem(),
	})
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, nil, err
	}

	var turnsLogger ports.TurnsLogger
	if paths.TurnsLogPath != "" {
		if tl, err := logging.NewAsyncTurnsLogger(paths.TurnsLogPath); err == nil {
			turnsLogger = tl
		} else {
			b.Logger.Warn("failed to initialize turns logger", "error", err)
		}
	}

	deps := b.buildAgentOrchestrator(paths, hManager, client, client, reg, pricingData, pricingOverrides, bus, cfg, b.Logger, turnsLogger, sessionProvider)

	return deps, hManager, cleanup, nil
}

func (b *Bootstrapper) buildHistoryManager(ctx stdctx.Context, paths *persistence.Paths) (*history.Manager, error) {
	hManager := history.NewManager(infra_persistence.NewOSFileSystem(), paths.HistoryPath, paths.HistoryArchivePath)
	if err := hManager.Load(ctx); err != nil {
		if !errors.Is(err, ports.ErrHistoryNotFound) {
			return nil, fmt.Errorf("error loading history: %w", err)
		}
	}
	return hManager, nil
}

func (b *Bootstrapper) buildToolRegistry(params infra_tools.ToolRegistrationParams) (tools.Registry, error) {
	reg := registry.New()
	params.Registry = reg

	if err := b.RegisterAllTools(params); err != nil {
		return nil, fmt.Errorf("error registering tools: %w", err)
	}

	// Infrastructure-specific tool registration
	if err := b.RegisterMetrics(reg, b.SM, params.LogFile, params.TraceFile, params.Model, params.Mode, params.PricingOverrides, params.SessionProvider.GetSettings()); err != nil {
		return nil, fmt.Errorf("error registering metrics tools: %w", err)
	}
	if err := b.SM.RegisterPolicyTools(reg, params.SessionProvider.GetSettings()); err != nil {
		return nil, fmt.Errorf("error registering policy tools: %w", err)
	}
	return reg, nil
}

func (b *Bootstrapper) buildSessionProvider(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config) (ports.SessionProvider, func(stdctx.Context) error, error) {
	var sessionProvider ports.SessionProvider
	state, err := b.NewSessionState(ctx, paths.ModeDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize session state: %w", err)
	}

	sessionProvider = state
	info := state.GetInfo()
	info.Model = cfg.Model
	info.Provider = cfg.SelectedProvider
	state.SetInfo(info)

	cleanup := func(stdctx.Context) error {
		if sessionProvider != nil {
			if err := sessionProvider.Close(); err != nil {
				_, _ = fmt.Fprintf(b.Stderr, "Warning: Failed to close session provider: %v\n", err)
				return err
			}
		}
		return nil
	}
	return sessionProvider, cleanup, nil
}

func (b *Bootstrapper) applySessionSecuritySettings(ctx stdctx.Context, sessionProvider ports.SessionProvider) {
	if val, err := sessionProvider.GetSettings().Get(ctx, "bypass_confirmation"); err == nil && val == "true" {
		b.SM.SetBypassActive(true)
	}

	// Load authorized paths from settings
	loadPathsFromSettings(ctx, sessionProvider.GetSettings(), "authorized_safe_paths", b.SM.RegisterSafePath, b.Logger)
	loadPathsFromSettings(ctx, sessionProvider.GetSettings(), "authorized_read_paths", b.SM.RegisterReadOnlyPath, b.Logger)
}

func loadPathsFromSettings(ctx stdctx.Context, kv ports.KVStore, key string, register func(string), logger *slog.Logger) {
	val, err := kv.Get(ctx, key)
	if err != nil || val == "" {
		return
	}

	var paths []string
	if err := json.Unmarshal([]byte(val), &paths); err != nil {
		logger.Error("failed to unmarshal "+key, "error", err, "value", val)
		return
	}

	for _, p := range paths {
		register(p)
	}
}

func (b *Bootstrapper) buildAgentOrchestrator(
	paths *persistence.Paths,
	hManager ports.HistoryManager,
	client llm.LLMClient,
	gw llm.LLMGateway,
	reg tools.Registry,
	pricingData pricing.PricingData,
	pricingOverrides map[string]pricing.ModelPricing,
	bus events.EventBus,
	cfg *config.Config,
	logger *slog.Logger,
	turnsLogger ports.TurnsLogger,
	sessionProvider ports.SessionProvider,
) ports.SessionDependencies {
	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	tracker := telemetry.NewSessionCostTracker(b.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	return &sessionDeps{
		paths:            paths,
		hManager:         hManager,
		client:           client,
		gw:               gw,
		reg:              reg,
		sm:               b.SM,
		tracker:          tracker,
		pricingData:      pricingData,
		pricingOverrides: pricingOverrides,
		bus:              bus,
		logger:           logger,
		turnsLogger:      turnsLogger,
		sessionProvider:  sessionProvider,
	}
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
	logger           *slog.Logger
	turnsLogger      ports.TurnsLogger
	sessionProvider  ports.SessionProvider
}

func (d *sessionDeps) GetGateway() llm.LLMGateway              { return d.gw }
func (d *sessionDeps) GetHistoryManager() ports.HistoryManager { return d.hManager }
func (d *sessionDeps) GetRegistry() tools.Registry             { return d.reg }
func (d *sessionDeps) GetSecurityManager() security.Manager    { return d.sm }
func (d *sessionDeps) GetEventBus() events.EventBus            { return d.bus }
func (d *sessionDeps) GetLogger() *slog.Logger                 { return d.logger }
func (d *sessionDeps) GetTurnsLogger() ports.TurnsLogger       { return d.turnsLogger }
func (d *sessionDeps) GetPaths() *persistence.Paths            { return d.paths }
func (d *sessionDeps) GetSessionProvider() ports.SessionProvider {
	return d.sessionProvider
}
func (d *sessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing {
	return d.pricingOverrides
}
func (d *sessionDeps) GetTracker() pricing.CostTracker     { return d.tracker }
func (d *sessionDeps) GetPricingData() pricing.PricingData { return d.pricingData }
func (d *sessionDeps) GetClient() llm.LLMClient            { return d.client }

// GetAgentFactory returns a factory for creating Chatter instances.
func (b *Bootstrapper) GetAgentFactory() ports.ChatterFactory {
	return factory.NewChatter
}

// FinalizeSession saves history and records session cost.
func (b *Bootstrapper) FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
	var errs []error

	// Finalize session
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

// setupSecurity configures the security manager with necessary paths.
func (b *Bootstrapper) setupSecurity(paths *persistence.Paths, configPath string) error {
	b.SM.RegisterSafePath(filepath.Join(b.HomeDir, "output"))
	b.SM.RegisterReadOnlyPath(configPath)
	return nil
}

// handleNewSession manages session rotation and cost recording for new sessions.
func (b *Bootstrapper) handleNewSession(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, b.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		_, _ = fmt.Fprintf(b.Stderr, "Warning: Failed to record session cost for backup (log may be missing/corrupt): %v\n", err)
	}

	// Critical path: always attempt to rotate the session
	retentionDays := 30
	if val, err := kvStore.Get(ctx, "backup_retention_days"); err == nil && val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			retentionDays = parsed
		}
	}
	if err := b.RotateSession(&infra_persistence.OSFileSystem{}, b.Stdout, *paths, retentionDays); err != nil {
		return fmt.Errorf("session rotation failed: %w", err)
	}
	return nil
}

func (b *Bootstrapper) GetHistoryManager(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
	paths, err := infra_persistence.InitializePaths(&infra_persistence.OSFileSystem{}, b.HomeDir, cfg.Mode)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session paths: %w", err)
	}

	hManager, err := b.buildHistoryManager(ctx, paths)
	if err != nil {
		return nil, err
	}

	return hManager, nil
}

// GetUnifiedHistoryProvider assembles the read-model for the history browser.
func (b *Bootstrapper) GetUnifiedHistoryProvider(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	paths, err := infra_persistence.InitializePaths(&infra_persistence.OSFileSystem{}, b.HomeDir, cfg.Mode)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session paths: %w", err)
	}

	archiveReader := history.NewJSONLArchiveReader(infra_persistence.NewOSFileSystem(), paths.HistoryArchivePath)

	return history.NewUnifiedProvider(archiveReader, hManager), nil
}

// GetSuggestionService initializes and returns the suggestion service.
func (b *Bootstrapper) GetSuggestionService(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
	tracker, err := history.NewGlobalPromptTracker(b.HomeDir)
	if err != nil {
		b.Logger.Warn("failed to initialize global prompt tracker, falling back to no-op", "error", err)
		tracker = history.NewNoOpTracker()
	}

	return suggestions.NewMultiSourceSuggestionService(ctx, infra_persistence.NewOSFileSystem(), tracker, recentHistory, b.Stderr)
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
