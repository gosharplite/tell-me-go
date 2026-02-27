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
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
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
	AgentFactory    ports.ChatterFactory
	HistoryRenderer ports.HistoryRenderer
	UIRenderer      ports.UIRenderer
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

// newSessionConfig creates a new sessionConfig with required parameters.
func newSessionConfig(configPath string, newSession bool, lastN int, rawOutput bool, prompt string, cfg *config.Config) ports.SessionConfig {
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
	HistoryManager   ports.HistoryManager
	Client           domain_llm.LLMClient
	Gateway          domain_llm.LLMGateway
	Registry         domaintools.IToolRegistry
	Tracker          domain_pricing.ICostTracker
	PricingData      domain_pricing.PricingData
	PricingOverrides map[string]domain_pricing.ModelPricing
	EventBus         events.EventBus
}

func (d *sessionDependencies) GetGateway() domain_llm.LLMGateway { return d.Gateway }
func (d *sessionDependencies) GetHistoryManager() ports.HistoryManager {
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

// newSessionDependencies creates a new sessionDependencies with all required components.
func newSessionDependencies(paths *persistence.Paths, hManager ports.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.IToolRegistry, tracker domain_pricing.ICostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus) ports.SessionDependencies {
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

// newOrchestrator creates a new orchestrator.
func newOrchestrator(homeDir, version string, loader config.ConfigLoader, sm domain_security.ISecurityManager, stdout, stderr io.Writer, factory ports.ChatterFactory, historyRenderer ports.HistoryRenderer, uiRenderer ports.UIRenderer) Orchestrator {
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
func (o *orchestrator) Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic Capturer) error {
	isTTY := ic.IsTTY(o.Stdout)
	o.renderHistory(sd.GetHistoryManager(), sc, isTTY)

	if sc.GetPrompt() == "" && sc.GetLastN() > 0 {
		return nil
	}

	cfg := sc.GetConfig()
	paths := sd.GetPaths()
	activeModel := cfg.GetActiveProvider().Model
	params := ports.NewChatterParams(
		ports.WithContext(ctx),
		ports.WithLoader(o.Loader),
		ports.WithGateway(sd.GetGateway()),
		ports.WithHistory(sd.GetHistoryManager()),
		ports.WithToolConfig(sd.GetRegistry()),
		ports.WithSecurityManager(o.SM),
		ports.WithStreamingDisabled(cfg.DisableStreaming),
		ports.WithEventBus(sd.GetEventBus()),
		ports.WithProvider(cfg.SelectedProvider),
		ports.WithModel(activeModel),
		ports.WithMode(cfg.Mode),
		ports.WithLogPath(paths.LogPath),
		ports.WithPricingOverrides(sd.GetPricingOverrides()),
		ports.WithCostTracker(sd.GetTracker()),
	)
	chatAgent, err := o.AgentFactory(params)
	if err != nil {
		return fmt.Errorf("failed to initialize agent: %w", err)
	}

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
	sess := ports.NewSession(sessionID, sd.GetHistoryManager())
	if err := chatAgent.Chat(ctx, sess, sc.GetPrompt()); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return nil
}

func (o *orchestrator) renderHistory(hManager ports.HistoryManager, sCfg ports.SessionConfig, isTTY bool) {
	if sCfg.GetLastN() <= 0 {
		return
	}
	cfg := sCfg.GetConfig()
	o.HistoryRenderer.Render(o.Stdout, hManager, sCfg.GetLastN(), ports.HistoryRenderOptions{
		Raw:          sCfg.GetRawOutput(),
		ShowThoughts: cfg.ShowThoughts,
		UseColor:     isTTY && !sCfg.GetRawOutput(),
	})
}

func (o *orchestrator) applyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, paths *persistence.Paths, pData domain_pricing.PricingData, capturer Capturer) error {
	cfg := sCfg.GetConfig()
	o.setupUIRendering(chatAgent, cfg, sCfg.GetRawOutput(), paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns); err != nil {
		return err
	}
	return chatAgent.SetTieredThreshold(ctx, cfg.ResolveTieredThreshold(pData))
}

func (o *orchestrator) setupUIRendering(chatAgent ports.Chatter, cfg *config.Config, rawOutput bool, logPath string, capturer Capturer) {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := newUIBridge(o.UIRenderer, cfg.ShowThoughts, cfg.ShowTools, rawOutput, useColor, logPath)
	chatAgent.Subscribe(bridge.handleEvent)
}

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	renderer     ports.UIRenderer
	showThoughts bool
	showTools    bool
	rawOutput    bool
	useColor     bool
	logFile      string
}

// newUIBridge creates a new uiBridge.
func newUIBridge(renderer ports.UIRenderer, showThoughts, showTools, rawOutput, useColor bool, logFile string) *uiBridge {
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

// RunParams contains all parameters needed to execute a chat session.
type RunParams struct {
	HomeDir         string
	Version         string
	Loader          config.ConfigLoader
	SM              domain_security.ISecurityManager
	Stdout          io.Writer
	Stderr          io.Writer
	AgentFactory    ports.ChatterFactory
	HistoryRenderer ports.HistoryRenderer
	UIRenderer      ports.UIRenderer
	ConfigPath      string
	NewSession      bool
	LastN           int
	RawOutput       bool
	Prompt          string
	Config          *config.Config
	Deps            ports.SessionDependencies
	Capturer        Capturer
}

// Run is the high-level entry point for running a chat session.
// It simplifies the public API by encapsulating internal component assembly.
func Run(ctx context.Context, params RunParams) error {
	orch := newOrchestrator(
		params.HomeDir,
		params.Version,
		params.Loader,
		params.SM,
		params.Stdout,
		params.Stderr,
		params.AgentFactory,
		params.HistoryRenderer,
		params.UIRenderer,
	)

	sCfg := newSessionConfig(
		params.ConfigPath,
		params.NewSession,
		params.LastN,
		params.RawOutput,
		params.Prompt,
		params.Config,
	)

	return orch.Run(ctx, sCfg, params.Deps, params.Capturer)
}
