// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
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
	SM              domain_security.Manager
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
	BackN      int
	RawOutput  bool
	Prompt     string
	Config     *config.Config
}

func (c *sessionConfig) GetPrompt() string         { return c.Prompt }
func (c *sessionConfig) GetLastN() int             { return c.LastN }
func (c *sessionConfig) GetBackN() int             { return c.BackN }
func (c *sessionConfig) GetRawOutput() bool        { return c.RawOutput }
func (c *sessionConfig) GetConfig() *config.Config { return c.Config }

// newSessionConfig creates a new sessionConfig with required parameters.
func newSessionConfig(configPath string, newSession bool, lastN, backN int, rawOutput bool, prompt string, cfg *config.Config) ports.SessionConfig {
	return &sessionConfig{
		ConfigPath: configPath,
		NewSession: newSession,
		LastN:      lastN,
		BackN:      backN,
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
	Registry         domaintools.Registry
	SecurityManager  domain_security.Manager
	Tracker          domain_pricing.CostTracker
	PricingData      domain_pricing.PricingData
	PricingOverrides map[string]domain_pricing.ModelPricing
	EventBus         events.EventBus
	Logger           *slog.Logger
}

func (d *sessionDependencies) GetGateway() domain_llm.LLMGateway { return d.Gateway }
func (d *sessionDependencies) GetHistoryManager() ports.HistoryManager {
	return d.HistoryManager
}
func (d *sessionDependencies) GetRegistry() domaintools.Registry { return d.Registry }
func (d *sessionDependencies) GetSecurityManager() domain_security.Manager {
	return d.SecurityManager
}
func (d *sessionDependencies) GetEventBus() events.EventBus { return d.EventBus }
func (d *sessionDependencies) GetLogger() *slog.Logger      { return d.Logger }
func (d *sessionDependencies) GetPaths() *persistence.Paths { return d.Paths }
func (d *sessionDependencies) GetPricingOverrides() map[string]domain_pricing.ModelPricing {
	return d.PricingOverrides
}
func (d *sessionDependencies) GetTracker() domain_pricing.CostTracker { return d.Tracker }
func (d *sessionDependencies) GetPricingData() domain_pricing.PricingData {
	return d.PricingData
}

// newSessionDependencies creates a new sessionDependencies with all required components.
func newSessionDependencies(paths *persistence.Paths, hManager ports.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.Registry, sm domain_security.Manager, tracker domain_pricing.CostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus, logger *slog.Logger) ports.SessionDependencies {
	return &sessionDependencies{
		Paths:            paths,
		HistoryManager:   hManager,
		Client:           client,
		Gateway:          gw,
		Registry:         reg,
		SecurityManager:  sm,
		Tracker:          tracker,
		PricingData:      pData,
		PricingOverrides: overrides,
		EventBus:         bus,
		Logger:           logger,
	}
}

// newOrchestrator creates a new orchestrator.
func newOrchestrator(homeDir, version string, loader config.ConfigLoader, sm domain_security.Manager, stdout, stderr io.Writer, factory ports.ChatterFactory, historyRenderer ports.HistoryRenderer, uiRenderer ports.UIRenderer) Orchestrator {
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
func (o *orchestrator) Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic ports.Capturer) error {
	cfg := sc.GetConfig()
	paths := sd.GetPaths()
	activeModel := cfg.GetActiveProvider().Model
	chatterCfg := ports.ChatterConfig{
		ProviderName: cfg.SelectedProvider,
		Model:        activeModel,
		Mode:         cfg.Mode,
		LogPath:      paths.LogPath,
		TracePath:    paths.TracePath,
	}
	chatAgent, err := o.AgentFactory(ctx, sd, chatterCfg)
	if err != nil {
		return fmt.Errorf("failed to initialize agent: %w", err)
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		if err := chatAgent.Shutdown(shutdownCtx); err != nil {
			_, _ = fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}
	}()

	cleanupUI, err := o.applyConfiguration(ctx, chatAgent, sc, paths, sd.GetPricingData(), ic)
	if cleanupUI != nil {
		defer cleanupUI()
	}
	if err != nil {
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

// Rollback deletes the specified number of turns from history.
func (o *orchestrator) Rollback(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies) error {
	if sc.GetBackN() <= 0 {
		return nil
	}

	actualRemoved, remainingTurns, remainingMsgs, err := sd.GetHistoryManager().RollbackTurns(ctx, sc.GetBackN())
	if err != nil {
		return fmt.Errorf("failed to rollback history: %w", err)
	}
	_, _ = fmt.Fprintf(o.Stdout, "⏪ Rolled back %d turns. History now contains %d turns (%d messages).\n",
		actualRemoved, remainingTurns, remainingMsgs)

	return nil
}

// RenderHistory renders the last N messages from history.
func (o *orchestrator) RenderHistory(hManager ports.HistoryManager, sCfg ports.SessionConfig, isTTY bool) {
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

func (o *orchestrator) applyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, paths *persistence.Paths, pData domain_pricing.PricingData, capturer ports.Capturer) (func(), error) {
	cfg := sCfg.GetConfig()
	cleanup := o.setupUIRendering(ctx, chatAgent, cfg, sCfg.GetRawOutput(), paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns); err != nil {
		return cleanup, err
	}
	return cleanup, chatAgent.SetTieredThreshold(ctx, cfg.ResolveTieredThreshold(pData))
}

func (o *orchestrator) setupUIRendering(ctx context.Context, chatAgent ports.Chatter, cfg *config.Config, rawOutput bool, logPath string, capturer ports.Capturer) func() {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := newUIBridge(ctx, o.UIRenderer, cfg.ShowThoughts, cfg.ShowTools, rawOutput, useColor, logPath)
	chatAgent.Subscribe(bridge.handleEvent)
	return bridge.Cleanup
}

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	ctx                 context.Context
	renderer            ports.UIRenderer
	showThoughts        bool
	showTools           bool
	rawOutput           bool
	useColor            bool
	logFile             string
	stopSpinner         func()
	isRendering         bool
	isWaitingForConsent bool
	activePhase         events.Event
	eventCh             chan events.Event
	done                chan struct{}
}

func (b *uiBridge) stopActiveSpinner() {
	stop := b.stopSpinner
	b.stopSpinner = nil

	if stop != nil {
		stop()
	}
}

func (b *uiBridge) resumeActiveSpinner() {
	phase := b.activePhase
	if phase != nil {
		b.startSpinnerForPhase(phase)
	}
}

// Cleanup stops any active spinner.
func (b *uiBridge) Cleanup() {
	close(b.done)
}

// newUIBridge creates a new uiBridge.
func newUIBridge(ctx context.Context, renderer ports.UIRenderer, showThoughts, showTools, rawOutput, useColor bool, logFile string) *uiBridge {
	b := &uiBridge{
		ctx:          ctx,
		renderer:     renderer,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		useColor:     useColor,
		logFile:      logFile,
		eventCh:      make(chan events.Event, 100),
		done:         make(chan struct{}),
	}
	go b.loop()
	return b
}

func (b *uiBridge) loop() {
	for {
		select {
		case e := <-b.eventCh:
			func() {
				defer func() {
					if r := recover(); r != nil {
						// Recover from UI panics to keep the bridge alive
					}
				}()
				b.processEvent(b.ctx, e)
			}()
		case <-b.done:
			b.stopActiveSpinner()
			return
		}
	}
}

// handleEvent processes a domain event and updates the UI.
func (b *uiBridge) handleEvent(ctx context.Context, e events.Event) {
	select {
	case b.eventCh <- e:
	case <-ctx.Done():
	case <-b.done:
	}
}

func (b *uiBridge) processEvent(ctx context.Context, e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		b.handleTurnStatus(ev)
	case events.InferenceStartedEvent, events.SummarizationStartedEvent, events.ToolExecutionStartedEvent, events.RetryWaitingEvent:
		b.handleSpinnerEvent(ev)
	case events.ConsentStartedEvent:
		b.isWaitingForConsent = true
		b.stopActiveSpinner()
	case events.ConsentFinishedEvent:
		b.isWaitingForConsent = false
		b.resumeActiveSpinner()
	case events.ResponseEvent:
		b.handleResponse(ev)
	case events.UsageMetricsEvent:
		b.handleUsageMetrics(ev)
	case events.ToolCallEvent, events.ToolResultEvent:
		b.handleToolEvents(ev)
	case events.TurnStarted:
		b.handleTurnStarted()
	case events.SystemMessageEvent, events.StatusUpdate:
		b.handleSystemMessage(ev)
	}
}

func (b *uiBridge) handleSystemMessage(e events.Event) {
	var msg, lvl string
	switch ev := e.(type) {
	case events.SystemMessageEvent:
		msg, lvl = ev.Message, ev.Level
	case events.StatusUpdate:
		msg, lvl = ev.Message, ev.Level
	default:
		return
	}
	b.stopActiveSpinner()
	b.renderer.LogSystemMessage(msg, lvl)
	b.resumeActiveSpinner()
}

func (b *uiBridge) handleSpinnerEvent(e events.Event) {
	b.activePhase = e
	b.startSpinnerForPhase(e)
}

func (b *uiBridge) startSpinnerForPhase(e events.Event) {
	switch ev := e.(type) {
	case events.InferenceStartedEvent:
		status := " Thinking..."
		if ev.Model != "" {
			status = fmt.Sprintf(" Thinking [%s]...", ev.Model)
		}
		b.transitionSpinner(func() func() {
			return b.renderer.StartSpinnerWithStatus(b.ctx, status)
		})
	case events.SummarizationStartedEvent:
		b.isRendering = false
		b.transitionSpinner(func() func() {
			return b.renderer.StartSpinnerWithStatus(b.ctx, " Compressing context...")
		})
	case events.ToolExecutionStartedEvent:
		b.isRendering = false // Reset state to allow tool spinner after inference

		status := " Executing tools..."
		if len(ev.ToolNames) == 1 {
			status = fmt.Sprintf(" Executing [%s]...", ev.ToolNames[0])
		} else if len(ev.ToolNames) > 1 {
			status = fmt.Sprintf(" Executing tools [%s]...", strings.Join(ev.ToolNames, ", "))
		}

		b.transitionSpinner(func() func() {
			return b.renderer.StartSpinnerWithMetrics(b.ctx, status)
		})
	case events.RetryWaitingEvent:
		b.isRendering = false
		b.transitionSpinner(func() func() {
			return b.renderer.StartSpinnerWithStatus(b.ctx, fmt.Sprintf(" Retrying in %v...", ev.Duration.Round(time.Second)))
		})
	}
}

func (b *uiBridge) handleTurnStatus(ev events.TurnStatusEvent) {
	b.isRendering = false
	b.activePhase = nil // Clear phase on new turn/header
	b.stopActiveSpinner()
	b.renderer.LogTurnStatus(ev.Status)
}

func (b *uiBridge) handleResponse(ev events.ResponseEvent) {
	b.isRendering = true
	b.activePhase = nil // Clear phase on response
	b.stopActiveSpinner()
	b.renderer.RenderResponse(ev.Content, b.showThoughts, b.rawOutput)
}

func (b *uiBridge) handleUsageMetrics(ev events.UsageMetricsEvent) {
	ctx := b.ensureContext(ev.Context, "UsageMetricsEvent")
	b.stopActiveSpinner()
	b.renderer.LogUsage(ctx, ev.Metrics, b.logFile, ev.StartTime)
	b.resumeActiveSpinner()
}

func (b *uiBridge) handleToolEvents(e events.Event) {
	switch ev := e.(type) {
	case events.ToolCallEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolCall(ev.Calls, ev.Turn, ev.MaxTurns, b.showTools)
		b.resumeActiveSpinner()
	case events.ToolResultEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolResult(ev.Name, ev.Result, b.showTools)
		b.resumeActiveSpinner()
	}
}

func (b *uiBridge) handleTurnStarted() {
	b.isRendering = false
	b.activePhase = nil
	b.stopActiveSpinner()
}

func (b *uiBridge) transitionSpinner(startFn func() func()) {
	if b.isRendering || b.isWaitingForConsent {
		return
	}

	b.stopActiveSpinner()
	b.stopSpinner = startFn()
}

func (b *uiBridge) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		b.renderer.LogSystemMessage(name+" missing context", "warn")
		return context.Background()
	}
	return ctx
}

// RunParams contains all parameters needed to execute a chat session.
type RunParams struct {
	HomeDir         string
	Version         string
	Loader          config.ConfigLoader
	SM              domain_security.Manager
	Stdout          io.Writer
	Stderr          io.Writer
	AgentFactory    ports.ChatterFactory
	HistoryRenderer ports.HistoryRenderer
	UIRenderer      ports.UIRenderer
	ConfigPath      string
	NewSession      bool
	LastN           int
	BackN           int
	RawOutput       bool
	Prompt          string
	Config          *config.Config
	Deps            ports.SessionDependencies
	Capturer        ports.Capturer
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
		params.BackN,
		params.RawOutput,
		params.Prompt,
		params.Config,
	)

	// Behavior 1: Render History (if requested)
	isTTY := params.Capturer.IsTTY(params.Stdout)
	orch.RenderHistory(params.Deps.GetHistoryManager(), sCfg, isTTY)

	// Behavior 2: Handle Rollback (if requested)
	if sCfg.GetBackN() > 0 {
		if err := orch.Rollback(ctx, sCfg, params.Deps); err != nil {
			return err
		}
		// If no prompt was provided alongside -b, exit early.
		if sCfg.GetPrompt() == "" {
			return nil
		}
	}

	// Behavior 3: Handle History-only display (early exit)
	if sCfg.GetPrompt() == "" && sCfg.GetLastN() > 0 {
		return nil
	}

	// Behavior 4: Main Orchestration Loop (Chat)
	return orch.Run(ctx, sCfg, params.Deps, params.Capturer)
}
