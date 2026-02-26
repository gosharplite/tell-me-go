// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/tools"
)

// Container defines the interface for building session dependencies and provides factories.
type Container interface {
	BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (services.SessionDependencies, *history.Manager, func(), error)
	GetAgentFactory() services.ChatterFactory
	FinalizeSession(ctx stdctx.Context, hManager services.HistoryManager, deps services.SessionDependencies, cfg *config.Config)
}

// bootstrapper handles the instantiation and wiring of system components.
type bootstrapper struct {
	HomeDir       string
	SM            security.ISecurityManager
	Version       string
	Stdout        io.Writer
	Stderr        io.Writer
	ClientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error)
}

// NewBootstrapper creates a new Container instance.
func NewBootstrapper(homeDir string, sm security.ISecurityManager, version string, stdout, stderr io.Writer, clientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error)) Container {
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
func (b *bootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (services.SessionDependencies, *history.Manager, func(), error) {
	paths, err := infra_persistence.InitializePaths(b.HomeDir, cfg.Mode)
	if err != nil {
		return nil, nil, nil, err
	}

	pricingOverrides := b.getPricingOverrides(cfg)
	b.setupSecurity(paths, configPath)
	if newSession {
		b.handleNewSession(ctx, paths, cfg, pricingOverrides)
	}

	hManager := history.NewManager(infra_persistence.NewOSFileSystem(), paths.HistoryPath, paths.HistoryArchivePath)
	if err := hManager.Load(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("error loading history: %w", err)
	}

	bus := events.NewSimpleEventBus()

	pricingData := telemetry.GetPricing(ctx, b.SM, filepath.Join(b.HomeDir, "output"))

	client, err := b.ClientFactory(cfg, pricingData, bus)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating client: %w", err)
	}

	gw, ok := client.(llm.LLMGateway)
	if !ok {
		return nil, nil, nil, fmt.Errorf("client does not implement LLMGateway")
	}

	reg := registry.New()

	executor := &exec.RealExecutor{}
	var sessionProvider services.ISessionProvider
	if state, err := infra_persistence.NewSessionState(ctx, paths.ModeDir); err == nil {
		sessionProvider = state
		// Inject model and provider ground truth for tools like get_session_info
		info := state.GetInfo()
		info.Model = cfg.Model
		info.Provider = cfg.SelectedProvider
		state.SetInfo(info)
	} else {
		fmt.Fprintf(b.Stderr, "Warning: Failed to initialize session state: %v\n", err)
	}
	validator := internal_security.NewCommandValidator(b.SM, capturer)

	cleanup := func() {
		if sessionProvider != nil {
			if err := sessionProvider.Close(); err != nil {
				fmt.Fprintf(b.Stderr, "Warning: Failed to close session provider: %v\n", err)
			}
		}
	}

	tools.RegisterAll(
		reg,
		b.SM,
		executor,
		validator,
		sessionProvider,
		paths.LogPath,
		cfg.Model,
		cfg.Mode,
		pricingOverrides,
		client,
		filepath.Join(b.HomeDir, "assets/generated"),
		bus,
		infra_persistence.NewOSFileSystem(),
	)

	// Infrastructure-specific tool registration
	telemetry.RegisterMetrics(reg, b.SM, paths.LogPath, cfg.Model, cfg.Mode, pricingOverrides)
	if ism, ok := b.SM.(*internal_security.SecurityManager); ok {
		internal_security.RegisterPolicy(reg, ism)
	}

	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	tracker := telemetry.NewSessionCostTracker(b.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	deps := orchestration.NewSessionDependencies(paths, hManager, client, gw, reg, tracker, pricingData, pricingOverrides, bus)

	return deps, hManager, cleanup, nil
}

// GetAgentFactory returns a factory for creating Chatter instances.
func (b *bootstrapper) GetAgentFactory() services.ChatterFactory {
	return func(params services.ChatterParams) services.Chatter {
		telemetry.RegisterTraceSubscriber(params.EventBus, params.LogPath)

		summarizer := infra_llm.NewSummarizer(params.Gateway, params.EventBus)

		return agent.New(params.Gateway, params.HistoryManager, params.Registry, params.SecurityManager, params.EventBus, summarizer, params.ProviderName,
			agent.WithPricing(params.Model, params.Mode, params.PricingOverrides),
			agent.WithSessionCostTracker(params.CostTracker),
			agent.WithInternalTools(),
			agent.WithLoader(params.Loader),
		)
	}
}

// FinalizeSession saves history and records session cost.
func (b *bootstrapper) FinalizeSession(ctx stdctx.Context, hManager services.HistoryManager, deps services.SessionDependencies, cfg *config.Config) {
	// Finalize session
	if saveErr := hManager.Save(ctx); saveErr != nil {
		fmt.Fprintf(b.Stderr, "Warning: Error saving history: %v\n", saveErr)
	}

	// Calculate and record cost
	if recordErr := telemetry.RecordSessionCost(ctx, b.SM, deps.GetTracker(), deps.GetPaths().LogPath, cfg.Model, cfg.Mode, "", deps.GetPricingOverrides()); recordErr != nil {
		fmt.Fprintf(b.Stderr, "Warning: Failed to record final session cost: %v\n", recordErr)
	}
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
func (b *bootstrapper) setupSecurity(paths *persistence.Paths, configPath string) {
	if sm, ok := b.SM.(*internal_security.SecurityManager); ok {
		sm.SetSafePathsFile(paths.SafePathsPath)
		sm.SetReadOnlyPathsFile(paths.ReadPathsPath)
		sm.SetBypassFile(paths.BypassPath)
		sm.SetCommandsLogFile(paths.CommandsLogPath)
		if err := sm.LoadSafePaths(); err != nil {
			fmt.Fprintf(b.Stderr, "Warning: Failed to load safe paths: %v\n", err)
		}
		if err := sm.LoadReadOnlyPaths(); err != nil {
			fmt.Fprintf(b.Stderr, "Warning: Failed to load read-only paths: %v\n", err)
		}
		sm.LoadBypassState()
		sm.RegisterSafePath(filepath.Join(b.HomeDir, "output"))
		sm.RegisterReadOnlyPath(configPath)
	}
}

// handleNewSession manages session rotation and cost recording for new sessions.
func (b *bootstrapper) handleNewSession(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, b.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		fmt.Fprintf(b.Stderr, "Warning: Failed to record session cost for backup: %v\n", err)
	}
	retentionDays := infra_persistence.LoadBackupRetentionDays(*paths)
	if err := infra_persistence.RotateSession(b.Stdout, *paths, retentionDays); err != nil {
		fmt.Fprintf(b.Stderr, "Warning: Session rotation failed: %v\n", err)
	}
}
