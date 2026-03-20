// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

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
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
)

// ConfigurableSecurityManager extends the domain security manager with configuration methods.
type ConfigurableSecurityManager interface {
	security.ISecurityManager
	SetCommandsLogFile(path string)
	SetSafePathsFile(path string)
	SetReadOnlyPathsFile(path string)
	SetBypassFile(path string)
	LoadSafePaths() error
	LoadReadOnlyPaths() error
	LoadBypassState()
	RegisterSafePath(path string)
	RegisterReadOnlyPath(path string)
	RegisterPolicyTools(r tools.IToolRegistry) error
}

// Container defines the interface for building session dependencies and provides factories.
type Container interface {
	BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (ports.SessionDependencies, *history.Manager, func(), error)
	GetAgentFactory() ports.ChatterFactory
	FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error
}

// bootstrapper handles the instantiation and wiring of system components.
type bootstrapper struct {
	HomeDir       string
	SM            ConfigurableSecurityManager
	Version       string
	Stdout        io.Writer
	Stderr        io.Writer
	ClientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)
}

// NewBootstrapper creates a new Container instance.
func NewBootstrapper(homeDir string, sm ConfigurableSecurityManager, version string, stdout, stderr io.Writer, clientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)) Container {
	if clientFactory == nil {
		clientFactory = infra_llm.NewClient
	}
	return &bootstrapper{
		HomeDir:       homeDir,
		SM:            sm,
		Version:       version,
		Stdout:        stdout,
		Stderr:        stderr,
		ClientFactory: clientFactory,
	}
}

// BuildSessionDependencies assembles all dependencies required for a chat session.
func (b *bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (ports.SessionDependencies, *history.Manager, func(), error) {
	paths, err := infra_persistence.InitializePaths(b.HomeDir, cfg.Mode)
	if err != nil {
		return nil, nil, nil, err
	}

	pricingOverrides := b.getPricingOverrides(cfg)
	if err := b.setupSecurity(paths, configPath); err != nil {
		return nil, nil, nil, err
	}
	if newSession {
		// Hard dependency: session rotation must complete before we continue.
		// Errors here MUST halt execution to prevent state corruption.
		if err := b.handleNewSession(ctx, paths, cfg, pricingOverrides); err != nil {
			return nil, nil, nil, fmt.Errorf("session initialization failed during rotation: %w", err)
		}
	}

	// Initialize commands log after session rotation to avoid file locks on Windows.
	b.SM.SetCommandsLogFile(paths.CommandsLogPath)

	hManager, err := b.buildHistoryManager(ctx, paths)
	if err != nil {
		return nil, nil, nil, err
	}

	bus := events.NewSimpleEventBus()

	pricingData := telemetry.GetPricing(ctx, b.SM, filepath.Join(b.HomeDir, "output"))

	client, err := b.ClientFactory(cfg, pricingData, bus, telemetry.NewSlogLogger())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating client: %w", err)
	}

	sessionProvider, cleanup, err := b.buildSessionProvider(ctx, paths, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	reg, err := b.buildToolRegistry(infra_tools.ToolRegistrationParams{
		SecurityManager:  b.SM,
		CommandExecutor:  &exec.RealExecutor{},
		CommandValidator: internal_security.NewCommandValidator(b.SM, capturer),
		SessionProvider:  sessionProvider,
		LogFile:          paths.LogPath,
		Model:            cfg.Model,
		Mode:             cfg.Mode,
		PricingOverrides: pricingOverrides,
		Client:           client,
		AssetsDir:        filepath.Join(b.HomeDir, "assets/generated"),
		EventBus:         bus,
		FileSystem:       infra_persistence.NewOSFileSystem(),
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}

	deps := b.buildAgentOrchestrator(paths, hManager, client, client, reg, pricingData, pricingOverrides, bus, cfg)

	return deps, hManager, cleanup, nil
}

func (b *bootstrapper) buildHistoryManager(ctx stdctx.Context, paths *persistence.Paths) (*history.Manager, error) {
	hManager := history.NewManager(infra_persistence.NewOSFileSystem(), paths.HistoryPath, paths.HistoryArchivePath)
	if err := hManager.Load(ctx); err != nil {
		if !errors.Is(err, ports.ErrHistoryNotFound) {
			return nil, fmt.Errorf("error loading history: %w", err)
		}
	}
	return hManager, nil
}

func (b *bootstrapper) buildToolRegistry(params infra_tools.ToolRegistrationParams) (tools.IToolRegistry, error) {
	reg := registry.New()
	params.Registry = reg

	if err := infra_tools.RegisterAll(params); err != nil {
		return nil, fmt.Errorf("error registering tools: %w", err)
	}

	// Infrastructure-specific tool registration
	if err := telemetry.RegisterMetrics(reg, b.SM, params.LogFile, params.Model, params.Mode, params.PricingOverrides); err != nil {
		return nil, fmt.Errorf("error registering metrics tools: %w", err)
	}
	if err := b.SM.RegisterPolicyTools(reg); err != nil {
		return nil, fmt.Errorf("error registering policy tools: %w", err)
	}
	return reg, nil
}

func (b *bootstrapper) buildSessionProvider(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config) (ports.ISessionProvider, func(), error) {
	var sessionProvider ports.ISessionProvider
	state, err := infra_persistence.NewSessionState(ctx, paths.ModeDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize session state: %w", err)
	}

	sessionProvider = state
	info := state.GetInfo()
	info.Model = cfg.Model
	info.Provider = cfg.SelectedProvider
	state.SetInfo(info)

	cleanup := func() {
		if sessionProvider != nil {
			if err := sessionProvider.Close(); err != nil {
				_, _ = fmt.Fprintf(b.Stderr, "Warning: Failed to close session provider: %v\n", err)
			}
		}
	}
	return sessionProvider, cleanup, nil
}

func (b *bootstrapper) buildAgentOrchestrator(
	paths *persistence.Paths,
	hManager ports.HistoryManager,
	client llm.LLMClient,
	gw llm.LLMGateway,
	reg tools.IToolRegistry,
	pricingData pricing.PricingData,
	pricingOverrides map[string]pricing.ModelPricing,
	bus events.EventBus,
	cfg *config.Config,
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
	}
}

type sessionDeps struct {
	paths            *persistence.Paths
	hManager         ports.HistoryManager
	client           llm.LLMClient
	gw               llm.LLMGateway
	reg              tools.IToolRegistry
	sm               security.ISecurityManager
	tracker          pricing.ICostTracker
	pricingData      pricing.PricingData
	pricingOverrides map[string]pricing.ModelPricing
	bus              events.EventBus
}

func (d *sessionDeps) GetGateway() llm.LLMGateway                    { return d.gw }
func (d *sessionDeps) GetHistoryManager() ports.HistoryManager       { return d.hManager }
func (d *sessionDeps) GetRegistry() tools.IToolRegistry              { return d.reg }
func (d *sessionDeps) GetSecurityManager() security.ISecurityManager { return d.sm }
func (d *sessionDeps) GetEventBus() events.EventBus                  { return d.bus }
func (d *sessionDeps) GetPaths() *persistence.Paths                  { return d.paths }
func (d *sessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing {
	return d.pricingOverrides
}
func (d *sessionDeps) GetTracker() pricing.ICostTracker    { return d.tracker }
func (d *sessionDeps) GetPricingData() pricing.PricingData { return d.pricingData }
func (d *sessionDeps) GetClient() llm.LLMClient            { return d.client }

// GetAgentFactory returns a factory for creating Chatter instances.
func (b *bootstrapper) GetAgentFactory() ports.ChatterFactory {
	return factory.NewChatter
}

// FinalizeSession saves history and records session cost.
func (b *bootstrapper) FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
	// Finalize session
	if saveErr := hManager.Save(ctx); saveErr != nil {
		return fmt.Errorf("error saving history: %w", saveErr)
	}

	// Calculate and record cost
	if recordErr := telemetry.RecordSessionCost(ctx, b.SM, deps.GetTracker(), deps.GetPaths().LogPath, cfg.Model, cfg.Mode, "", deps.GetPricingOverrides()); recordErr != nil {
		return fmt.Errorf("failed to record final session cost: %w", recordErr)
	}
	return nil
}

// getPricingOverrides extracts pricing overrides from the configuration.
func (b *bootstrapper) getPricingOverrides(cfg *config.Config) map[string]pricing.ModelPricing {
	pricingOverrides := make(map[string]pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

// setupSecurity configures the security manager with necessary paths and bypass states.
func (b *bootstrapper) setupSecurity(paths *persistence.Paths, configPath string) error {
	b.SM.SetSafePathsFile(paths.SafePathsPath)
	b.SM.SetReadOnlyPathsFile(paths.ReadPathsPath)
	b.SM.SetBypassFile(paths.BypassPath)
	if err := b.SM.LoadSafePaths(); err != nil {
		return fmt.Errorf("failed to load safe paths: %w", err)
	}
	if err := b.SM.LoadReadOnlyPaths(); err != nil {
		return fmt.Errorf("failed to load read-only paths: %w", err)
	}
	b.SM.LoadBypassState()
	b.SM.RegisterSafePath(filepath.Join(b.HomeDir, "output"))
	b.SM.RegisterReadOnlyPath(configPath)
	return nil
}

// handleNewSession manages session rotation and cost recording for new sessions.
func (b *bootstrapper) handleNewSession(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) error {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, b.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		return fmt.Errorf("failed to record session cost for backup: %w", err)
	}
	retentionDays := infra_persistence.LoadBackupRetentionDays(*paths)
	if err := infra_persistence.RotateSession(b.Stdout, *paths, retentionDays); err != nil {
		return fmt.Errorf("session rotation failed: %w", err)
	}
	return nil
}
