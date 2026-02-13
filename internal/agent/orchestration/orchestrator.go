// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// iCapturer defines the interface for UI interactions that the orchestrator needs.
type iCapturer interface {
	IsTTY(v any) bool
}

// orchestrator manages the session lifecycle and agent execution.
type orchestrator struct {
	HomeDir         string
	Version         string
	Loader          config.ConfigLoader
	SM              domain_security.ISecurityManager
	Stdout          io.Writer
	Stderr          io.Writer
	AgentFactory    func(loader config.ConfigLoader, client domain_llm.LLMGateway, hManager services.HistoryManager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) any
	HistoryRenderer services.HistoryRenderer
	UIRenderer      services.UIRenderer
}

// sessionConfig contains configuration for a single session execution.
type sessionConfig struct {
	ConfigPath string
	NewSession bool
	LastN      int
	RawOutput  bool
	Prompt     string
	Config     *config.Config
}

// NewSessionConfig creates a new sessionConfig with required parameters.
func NewSessionConfig(configPath string, newSession bool, lastN int, rawOutput bool, prompt string, cfg *config.Config) any {
	return &sessionConfig{
		ConfigPath: configPath,
		NewSession: newSession,
		LastN:      lastN,
		RawOutput:  rawOutput,
		Prompt:     prompt,
		Config:     cfg,
	}
}

// sessionDependencies holds the required components for a session.
type sessionDependencies struct {
	Paths            *persistence.Paths
	HistoryManager   services.HistoryManager
	Client           domain_llm.LLMClient
	Gateway          domain_llm.LLMGateway
	Registry         domaintools.IToolRegistry
	Tracker          domain_pricing.ICostTracker
	PricingData      domain_pricing.PricingData
	PricingOverrides map[string]domain_pricing.ModelPricing
	EventBus         events.EventBus
}

// NewSessionDependencies creates a new sessionDependencies with all required components.
func NewSessionDependencies(paths *persistence.Paths, hManager services.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.IToolRegistry, tracker domain_pricing.ICostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus) any {
	return &sessionDependencies{
		Paths:            paths,
		HistoryManager:   hManager,
		Client:           client,
		Gateway:          gw,
		Registry:         reg,
		Tracker:          tracker,
		PricingData:      pData,
		PricingOverrides: overrides,
		EventBus:         bus,
	}
}

// GetEventBus returns the event bus.
func (d *sessionDependencies) GetEventBus() events.EventBus { return d.EventBus }

// GetTracker returns the cost tracker.
func (d *sessionDependencies) GetTracker() domain_pricing.ICostTracker { return d.Tracker }

// GetPaths returns the paths.
func (d *sessionDependencies) GetPaths() *persistence.Paths { return d.Paths }

// GetPricingOverrides returns the pricing overrides.
func (d *sessionDependencies) GetPricingOverrides() map[string]domain_pricing.ModelPricing {
	return d.PricingOverrides
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(homeDir, version string, loader config.ConfigLoader, sm domain_security.ISecurityManager, stdout, stderr io.Writer, factory any, historyRenderer services.HistoryRenderer, uiRenderer services.UIRenderer) *orchestrator {
	var agentFactory func(config.ConfigLoader, domain_llm.LLMGateway, services.HistoryManager, domaintools.IToolRegistry, domain_security.ISecurityManager, bool, events.EventBus, string, string, string, map[string]domain_pricing.ModelPricing, domain_pricing.ICostTracker) any
	if factory != nil {
		agentFactory = factory.(func(config.ConfigLoader, domain_llm.LLMGateway, services.HistoryManager, domaintools.IToolRegistry, domain_security.ISecurityManager, bool, events.EventBus, string, string, string, map[string]domain_pricing.ModelPricing, domain_pricing.ICostTracker) any)
	}
	return &orchestrator{
		HomeDir:         homeDir,
		Version:         version,
		Loader:          loader,
		SM:              sm,
		Stdout:          stdout,
		Stderr:          stderr,
		AgentFactory:    agentFactory,
		HistoryRenderer: historyRenderer,
		UIRenderer:      uiRenderer,
	}
}

// Run executes the session orchestration.
func (o *orchestrator) Run(ctx context.Context, sCfg any, deps any, capturer any) error {
	sc := sCfg.(*sessionConfig)
	sd := deps.(*sessionDependencies)
	ic := capturer.(iCapturer)

	isTTY := ic.IsTTY(o.Stdout)
	o.renderHistory(sd.HistoryManager, sc, isTTY)

	if sc.Prompt == "" && sc.LastN > 0 {
		return nil
	}

	chatAgentRaw := o.AgentFactory(o.Loader, sd.Gateway, sd.HistoryManager, sd.Registry, o.SM, sc.Config.DisableStreaming, sd.EventBus, sc.Config.Model, sc.Config.Mode, sd.Paths.LogPath, sd.PricingOverrides, sd.Tracker)
	chatAgent := chatAgentRaw.(chatter)

	defer func() {
		if err := chatAgent.Shutdown(ctx); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}
	}()

	if err := o.applyConfiguration(ctx, chatAgent, sc, sd.Paths, sd.PricingData, ic); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	sess := NewSession(sessionID, sd.HistoryManager)
	if err := chatAgent.Chat(ctx, sess, sc.Prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return nil
}

func (o *orchestrator) renderHistory(hManager services.HistoryManager, sCfg *sessionConfig, isTTY bool) {
	if sCfg.LastN <= 0 {
		return
	}
	o.HistoryRenderer.Render(o.Stdout, hManager, sCfg.LastN, services.HistoryRenderOptions{
		Raw:          sCfg.RawOutput,
		ShowThoughts: sCfg.Config.ShowThoughts,
		UseColor:     isTTY && !sCfg.RawOutput,
	})
}

func (o *orchestrator) applyConfiguration(ctx context.Context, chatAgent chatter, sCfg *sessionConfig, paths *persistence.Paths, pData domain_pricing.PricingData, capturer iCapturer) error {
	o.setupUIRendering(chatAgent, sCfg.Config, sCfg.RawOutput, paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, sCfg.Config.MaxToolTurns, sCfg.Config.ResolveContextWindow(), sCfg.Config.MaxHistoryTurns); err != nil {
		return err
	}
	return chatAgent.SetTieredThreshold(ctx, sCfg.Config.ResolveTieredThreshold(pData))
}

func (o *orchestrator) setupUIRendering(chatAgent chatter, cfg *config.Config, rawOutput bool, logPath string, capturer iCapturer) {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := newUIBridge(o.UIRenderer, cfg.ShowThoughts, cfg.ShowTools, rawOutput, useColor, logPath)
	chatAgent.Subscribe(bridge.handleEvent)
}

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	renderer     services.UIRenderer
	showThoughts bool
	showTools    bool
	rawOutput    bool
	useColor     bool
	logFile      string
}

// newUIBridge creates a new uiBridge.
func newUIBridge(renderer services.UIRenderer, showThoughts, showTools, rawOutput, useColor bool, logFile string) *uiBridge {
	return &uiBridge{
		renderer:     renderer,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		useColor:     useColor,
		logFile:      logFile,
	}
}

// handleEvent processes a domain event and updates the UI.
func (b *uiBridge) handleEvent(e events.Event) {
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

func (b *uiBridge) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		b.renderer.LogSystemMessage(name+" missing context", "warn")
		return context.Background()
	}
	return ctx
}

func (b *uiBridge) relayStream(ctx context.Context, stream <-chan *domain_llm.Content, uiCh chan<- *domain_llm.Content) {
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
