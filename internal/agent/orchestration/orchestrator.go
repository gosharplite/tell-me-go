// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

// ICapturer defines the interface for UI interactions that the Orchestrator needs.
type ICapturer interface {
	IsTTY(v any) bool
}

// Orchestrator manages the session lifecycle and agent execution.
type Orchestrator struct {
	HomeDir string
	Version string
	SM      domain_security.ISecurityManager
	Stdout  io.Writer
	Stderr  io.Writer

	AgentFactory  func(client *llm.Client, hManager *history.Manager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) Chatter
	ClientFactory func(cfg *config.Config, pricing domain_pricing.PricingData, bus events.EventBus) (*llm.Client, error)
}

// SessionConfig contains configuration for a single session execution.
type SessionConfig struct {
	ConfigPath string
	NewSession bool
	LastN      int
	RawOutput  bool
	Prompt     string
	Config     *config.Config
}

// sessionDeps consolidates internal dependencies for a session.
type sessionDeps struct {
	paths            *persistence.Paths
	hManager         *history.Manager
	client           *llm.Client
	registry         domaintools.IToolRegistry
	tracker          domain_pricing.ICostTracker
	pData            domain_pricing.PricingData
	pricingOverrides map[string]domain_pricing.ModelPricing
	bus              events.EventBus
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(homeDir, version string, sm domain_security.ISecurityManager, stdout, stderr io.Writer) *Orchestrator {
	return &Orchestrator{
		HomeDir: homeDir,
		Version: version,
		SM:      sm,
		Stdout:  stdout,
		Stderr:  stderr,
	}
}

// Run executes the session orchestration.
func (o *Orchestrator) Run(ctx context.Context, sCfg *SessionConfig, capturer ICapturer) error {
	if o.AgentFactory == nil {
		return fmt.Errorf("AgentFactory must be set")
	}
	if o.ClientFactory == nil {
		return fmt.Errorf("ClientFactory must be set")
	}

	deps, err := o.prepareSession(ctx, sCfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := deps.bus.Shutdown(ctx); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Failed to shutdown event bus: %v\n", err)
		}
	}()

	isTTY := capturer.IsTTY(o.Stdout)
	o.renderHistory(deps.hManager, sCfg, isTTY)

	if sCfg.Prompt == "" && sCfg.LastN > 0 {
		return nil
	}

	chatAgent := o.AgentFactory(deps.client, deps.hManager, deps.registry, o.SM, sCfg.Config.DisableStreaming, deps.bus, sCfg.Config.Model, sCfg.Config.Mode, deps.paths.LogPath, deps.pricingOverrides, deps.tracker)
	defer func() {
		if err := chatAgent.Shutdown(ctx); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}
	}()

	if err := o.applyConfiguration(ctx, chatAgent, sCfg, deps.paths, deps.pData, capturer); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	sess := NewSession(sessionID, deps.hManager)
	if err := chatAgent.Chat(ctx, sess, sCfg.Prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return o.finalizeSession(ctx, chatAgent, deps.hManager, *deps.paths, sCfg.Config, deps.pricingOverrides)
}

func (o *Orchestrator) prepareSession(ctx context.Context, sCfg *SessionConfig) (*sessionDeps, error) {
	paths, err := persistence.InitializePaths(o.HomeDir, sCfg.Config.Mode)
	if err != nil {
		return nil, err
	}

	pricingOverrides := o.getPricingOverrides(sCfg.Config)
	o.setupSecurity(paths, sCfg.ConfigPath)
	if sCfg.NewSession {
		o.handleNewSession(ctx, paths, sCfg.Config, pricingOverrides)
	}

	return o.initializeDependencies(ctx, paths, sCfg.Config, pricingOverrides)
}

func (o *Orchestrator) initializeDependencies(ctx context.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) (*sessionDeps, error) {
	hManager := history.NewManager(paths.HistoryPath)
	if err := hManager.Load(ctx); err != nil {
		return nil, fmt.Errorf("error loading history: %w", err)
	}

	bus := events.NewSimpleEventBus()

	pricingData := telemetry.GetPricing(ctx, o.SM, filepath.Join(o.HomeDir, "output"))

	client, err := o.ClientFactory(cfg, pricingData, bus)
	if err != nil {
		return nil, fmt.Errorf("error creating client: %w", err)
	}

	registry := o.SetupRegistry(client, cfg, paths, pricingOverrides, bus)
	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	tracker := telemetry.NewSessionCostTracker(o.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	return &sessionDeps{
		paths:            paths,
		hManager:         hManager,
		client:           client,
		registry:         registry,
		tracker:          tracker,
		pData:            pricingData,
		pricingOverrides: pricingOverrides,
		bus:              bus,
	}, nil
}

func (o *Orchestrator) finalizeSession(ctx context.Context, chatAgent Chatter, hManager *history.Manager, paths persistence.Paths, cfg *config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) error {
	if err := hManager.Save(ctx); err != nil {
		return fmt.Errorf("error saving history: %w", err)
	}
	if err := telemetry.RecordSessionCost(ctx, o.SM, chatAgent.GetCostTracker(), paths.LogPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(o.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}
	return nil
}

func (o *Orchestrator) getPricingOverrides(cfg *config.Config) map[string]domain_pricing.ModelPricing {
	pricingOverrides := make(map[string]domain_pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (o *Orchestrator) setupSecurity(paths *persistence.Paths, configPath string) {
	if sm, ok := o.SM.(*internal_security.SecurityManager); ok {
		sm.SetSafePathsFile(paths.SafePathsPath)
		sm.SetReadOnlyPathsFile(paths.ReadPathsPath)
		sm.SetBypassFile(paths.BypassPath)
		sm.SetCommandsLogFile(paths.CommandsLogPath)
		if err := sm.LoadSafePaths(); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Failed to load safe paths: %v\n", err)
		}
		if err := sm.LoadReadOnlyPaths(); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Failed to load read-only paths: %v\n", err)
		}
		sm.LoadBypassState()
		sm.RegisterSafePath(filepath.Join(o.HomeDir, "output"))
		sm.RegisterReadOnlyPath(configPath)
	}
}

func (o *Orchestrator) handleNewSession(ctx context.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, o.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		fmt.Fprintf(o.Stderr, "Warning: Failed to record session cost for backup: %v\n", err)
	}
	retentionDays := persistence.LoadBackupRetentionDays(*paths)
	if err := persistence.RotateSession(o.Stdout, *paths, retentionDays); err != nil {
		fmt.Fprintf(o.Stderr, "Warning: Session rotation failed: %v\n", err)
	}
}

func (o *Orchestrator) renderHistory(hManager *history.Manager, sCfg *SessionConfig, isTTY bool) {
	if sCfg.LastN <= 0 {
		return
	}
	ui.History(o.Stdout, hManager, sCfg.LastN, ui.RenderOptions{
		Raw:          sCfg.RawOutput,
		ShowThoughts: sCfg.Config.ShowThoughts,
		UseColor:     isTTY && !sCfg.RawOutput,
	})
}

func (o *Orchestrator) applyConfiguration(ctx context.Context, chatAgent Chatter, sCfg *SessionConfig, paths *persistence.Paths, pData domain_pricing.PricingData, capturer ICapturer) error {
	o.setupUIRendering(chatAgent, sCfg.Config, sCfg.RawOutput, paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, sCfg.Config.MaxToolTurns, sCfg.Config.ResolveContextWindow(), sCfg.Config.MaxHistoryTurns); err != nil {
		return err
	}
	return chatAgent.SetTieredThreshold(ctx, sCfg.Config.ResolveTieredThreshold(pData))
}

func (o *Orchestrator) setupUIRendering(chatAgent Chatter, cfg *config.Config, rawOutput bool, logPath string, capturer ICapturer) {
	renderer := ui.NewRenderer(o.SM, o.Stdout, o.Stderr)
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	renderer.SetUseColor(useColor)
	bridge := NewUIBridge(renderer, cfg.ShowThoughts, cfg.ShowTools, rawOutput, useColor, logPath)
	chatAgent.Subscribe(bridge.HandleEvent)
}

func (o *Orchestrator) SetupRegistry(client *llm.Client, cfg *config.Config, paths *persistence.Paths, pricingOverrides map[string]domain_pricing.ModelPricing, bus events.EventBus) domaintools.IToolRegistry {
	reg := registry.New()

	tools.RegisterAll(
		reg,
		o.SM,
		paths.ModeDir,
		paths.LogPath,
		cfg.Model,
		cfg.Mode,
		pricingOverrides,
		client,
		filepath.Join(o.HomeDir, "assets/generated"),
		bus,
	)

	return reg
}

// UIBridge translates domain events into UI updates.
type UIBridge struct {
	renderer     ui.UIRenderer
	showThoughts bool
	showTools    bool
	rawOutput    bool
	useColor     bool
	logFile      string
}

// NewUIBridge creates a new UIBridge.
func NewUIBridge(renderer ui.UIRenderer, showThoughts, showTools, rawOutput, useColor bool, logFile string) *UIBridge {
	return &UIBridge{
		renderer:     renderer,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		useColor:     useColor,
		logFile:      logFile,
	}
}

// HandleEvent processes a domain event and updates the UI.
func (b *UIBridge) HandleEvent(e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		b.renderer.LogTurnStatus(ev.Status)
	case events.ResponseStreamEvent:
		ctx := b.ensureContext(ev.Context, "ResponseStreamEvent")
		uiCh, uiFinalize := b.renderer.StreamResponse(ctx, b.showThoughts, b.rawOutput)
		b.relayStream(ctx, ev.Stream, uiCh)
		_ = uiFinalize()
	case events.UsageMetricsEvent:
		ctx := b.ensureContext(ev.Context, "UsageMetricsEvent")
		b.renderer.LogUsage(ctx, ev.Metrics, b.logFile, ev.StartTime)
	case events.ToolCallEvent:
		b.renderer.LogToolCall(ev.Calls, ev.Turn, ev.MaxTurns, b.showTools)
	case events.ToolResultEvent:
		b.renderer.LogToolResult(ev.Name, ev.Result, b.showTools)
	case events.SystemMessageEvent:
		b.renderer.LogSystemMessage(ev.Message, ev.Level)
	case events.StatusUpdate:
		b.renderer.LogSystemMessage(ev.Message, ev.Level)
	}
}

func (b *UIBridge) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		b.renderer.LogSystemMessage(name+" missing context", "warn")
		return context.Background()
	}
	return ctx
}

func (b *UIBridge) relayStream(ctx context.Context, stream <-chan *domain_llm.Content, uiCh chan<- *domain_llm.Content) {
	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-stream:
			if !ok {
				return
			}
			select {
			case uiCh <- c:
			case <-ctx.Done():
				return
			}
		}
	}
}
