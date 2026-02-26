// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// orchestrator manages the session lifecycle and agent execution.
type orchestrator struct {
	HomeDir         string
	Version         string
	Loader          config.ConfigLoader
	SM              domain_security.ISecurityManager
	Stdout          io.Writer
	Stderr          io.Writer
	AgentFactory    services.ChatterFactory
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

func (c *sessionConfig) GetPrompt() string         { return c.Prompt }
func (c *sessionConfig) GetLastN() int             { return c.LastN }
func (c *sessionConfig) GetRawOutput() bool        { return c.RawOutput }
func (c *sessionConfig) GetConfig() *config.Config { return c.Config }

// NewSessionConfig creates a new sessionConfig with required parameters.
func NewSessionConfig(configPath string, newSession bool, lastN int, rawOutput bool, prompt string, cfg *config.Config) services.SessionConfig {
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

func (d *sessionDependencies) GetGateway() domain_llm.LLMGateway { return d.Gateway }
func (d *sessionDependencies) GetHistoryManager() services.HistoryManager {
	return d.HistoryManager
}
func (d *sessionDependencies) GetRegistry() domaintools.IToolRegistry { return d.Registry }
func (d *sessionDependencies) GetEventBus() events.EventBus           { return d.EventBus }
func (d *sessionDependencies) GetPaths() *persistence.Paths           { return d.Paths }
func (d *sessionDependencies) GetPricingOverrides() map[string]domain_pricing.ModelPricing {
	return d.PricingOverrides
}
func (d *sessionDependencies) GetTracker() domain_pricing.ICostTracker { return d.Tracker }
func (d *sessionDependencies) GetPricingData() domain_pricing.PricingData {
	return d.PricingData
}

// NewSessionDependencies creates a new sessionDependencies with all required components.
func NewSessionDependencies(paths *persistence.Paths, hManager services.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.IToolRegistry, tracker domain_pricing.ICostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus) services.SessionDependencies {
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

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(homeDir, version string, loader config.ConfigLoader, sm domain_security.ISecurityManager, stdout, stderr io.Writer, factory services.ChatterFactory, historyRenderer services.HistoryRenderer, uiRenderer services.UIRenderer) Orchestrator {
	return &orchestrator{
		HomeDir:         homeDir,
		Version:         version,
		Loader:          loader,
		SM:              sm,
		Stdout:          stdout,
		Stderr:          stderr,
		AgentFactory:    factory,
		HistoryRenderer: historyRenderer,
		UIRenderer:      uiRenderer,
	}
}

// Run executes the session orchestration.
func (o *orchestrator) Run(ctx context.Context, sc services.SessionConfig, sd services.SessionDependencies, ic Capturer) error {
	isTTY := ic.IsTTY(o.Stdout)
	o.renderHistory(sd.GetHistoryManager(), sc, isTTY)

	if sc.GetPrompt() == "" && sc.GetLastN() > 0 {
		return nil
	}

	cfg := sc.GetConfig()
	paths := sd.GetPaths()
	activeModel := cfg.GetActiveProvider().Model
	params := services.NewChatterParams(
		services.WithContext(ctx),
		services.WithLoader(o.Loader),
		services.WithGateway(sd.GetGateway()),
		services.WithHistory(sd.GetHistoryManager()),
		services.WithToolConfig(sd.GetRegistry()),
		services.WithSecurityManager(o.SM),
		services.WithStreamingDisabled(cfg.DisableStreaming),
		services.WithEventBus(sd.GetEventBus()),
		services.WithProvider(cfg.SelectedProvider),
		services.WithModel(activeModel),
		services.WithMode(cfg.Mode),
		services.WithLogPath(paths.LogPath),
		services.WithPricingOverrides(sd.GetPricingOverrides()),
		services.WithCostTracker(sd.GetTracker()),
	)
	chatAgent := o.AgentFactory(params)

	defer func() {
		if err := chatAgent.Shutdown(ctx); err != nil {
			fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}
	}()

	if err := o.applyConfiguration(ctx, chatAgent, sc, paths, sd.GetPricingData(), ic); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	b := make([]byte, 8)
	var sessionID string
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if entropy source fails
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	} else {
		sessionID = fmt.Sprintf("session-%s", hex.EncodeToString(b))
	}
	sess := services.NewSession(sessionID, sd.GetHistoryManager())
	if err := chatAgent.Chat(ctx, sess, sc.GetPrompt()); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return nil
}

func (o *orchestrator) renderHistory(hManager services.HistoryManager, sCfg services.SessionConfig, isTTY bool) {
	if sCfg.GetLastN() <= 0 {
		return
	}
	cfg := sCfg.GetConfig()
	o.HistoryRenderer.Render(o.Stdout, hManager, sCfg.GetLastN(), services.HistoryRenderOptions{
		Raw:          sCfg.GetRawOutput(),
		ShowThoughts: cfg.ShowThoughts,
		UseColor:     isTTY && !sCfg.GetRawOutput(),
	})
}

func (o *orchestrator) applyConfiguration(ctx context.Context, chatAgent services.Chatter, sCfg services.SessionConfig, paths *persistence.Paths, pData domain_pricing.PricingData, capturer Capturer) error {
	cfg := sCfg.GetConfig()
	o.setupUIRendering(chatAgent, cfg, sCfg.GetRawOutput(), paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns); err != nil {
		return err
	}
	return chatAgent.SetTieredThreshold(ctx, cfg.ResolveTieredThreshold(pData))
}

func (o *orchestrator) setupUIRendering(chatAgent services.Chatter, cfg *config.Config, rawOutput bool, logPath string, capturer Capturer) {
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
