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
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
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
	Clock           clock.Clock
	EntropySource   io.Reader
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
func newOrchestrator(homeDir, version string, loader config.ConfigLoader, sm domain_security.Manager, stdout, stderr io.Writer, factory ports.ChatterFactory, historyRenderer ports.HistoryRenderer, uiRenderer ports.UIRenderer, clk clock.Clock, entropy io.Reader) Orchestrator {
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
		Clock:           clk,
		EntropySource:   entropy,
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

	bridge, err := o.applyConfiguration(ctx, chatAgent, sc, sd, ic)
	// Single defer to guarantee deterministic teardown order:
	// Stop Producers first, then Consumers.
	defer func() {
		// 1. Stop Producers (Agent) first
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		if err := chatAgent.Shutdown(shutdownCtx); err != nil {
			_, _ = fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}

		// 2. Clean up Consumer (UI) second
		if bridge != nil {
			bridge.CloseInput()
			bridge.Cleanup()
		}
	}()
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	b := make([]byte, 8)
	var sessionID string
	if _, err := io.ReadFull(o.EntropySource, b); err != nil {
		_, _ = fmt.Fprintf(o.Stderr, "[WARN] Entropy source failure, degrading to time-based session ID: %v\n", err)
		sessionID = fmt.Sprintf("session-%d", o.Clock.Now().UnixNano())
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
	cfg := sCfg.GetConfig()
	o.HistoryRenderer.Render(o.Stdout, hManager, sCfg.GetLastN(), ports.HistoryRenderOptions{
		Raw:          sCfg.GetRawOutput(),
		ShowThoughts: cfg.ShowThoughts,
		UseColor:     isTTY && !sCfg.GetRawOutput(),
	})
}

func (o *orchestrator) applyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, sd ports.SessionDependencies, capturer ports.Capturer) (*uiBridge, error) {
	cfg := sCfg.GetConfig()
	paths := sd.GetPaths()
	pData := sd.GetPricingData()
	logger := sd.GetLogger()
	bridge := o.setupUIRendering(ctx, chatAgent, cfg, sCfg.GetRawOutput(), paths.LogPath, logger, capturer)
	if err := chatAgent.SetLimits(ctx, cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns); err != nil {
		return bridge, err
	}
	return bridge, chatAgent.SetTieredThreshold(ctx, cfg.ResolveTieredThreshold(pData))
}

func (o *orchestrator) setupUIRendering(ctx context.Context, chatAgent ports.Chatter, cfg *config.Config, rawOutput bool, logPath string, logger *slog.Logger, capturer ports.Capturer) *uiBridge {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := newUIBridge(ctx, o.UIRenderer,
		WithBridgeThoughts(cfg.ShowThoughts),
		WithBridgeTools(cfg.ShowTools),
		WithBridgeRawOutput(rawOutput),
		WithBridgeColor(useColor),
		WithBridgeLogFile(logPath),
		WithBridgeLogger(logger),
	)
	chatAgent.Subscribe(bridge.handleEvent)
	return bridge
}

// BridgeOption configures a uiBridge instance.
type BridgeOption func(*uiBridge)

// WithBridgeThoughts enables or disables thought rendering.
func WithBridgeThoughts(show bool) BridgeOption {
	return func(b *uiBridge) { b.showThoughts = show }
}

// WithBridgeTools enables or disables tool call rendering.
func WithBridgeTools(show bool) BridgeOption {
	return func(b *uiBridge) { b.showTools = show }
}

// WithBridgeRawOutput enables or disables raw output mode.
func WithBridgeRawOutput(raw bool) BridgeOption {
	return func(b *uiBridge) { b.rawOutput = raw }
}

// WithBridgeColor enables or disables ANSI color support.
func WithBridgeColor(color bool) BridgeOption {
	return func(b *uiBridge) { b.useColor = color }
}

// WithBridgeLogFile sets the file path for logging usage metrics.
func WithBridgeLogFile(path string) BridgeOption {
	return func(b *uiBridge) { b.logFile = path }
}

// WithBridgeLogger sets the structured logger.
func WithBridgeLogger(l *slog.Logger) BridgeOption {
	return func(b *uiBridge) { b.logger = l }
}

// WithBridgeCleanupTimeout sets the duration to wait for the bridge to drain events during cleanup.
func WithBridgeCleanupTimeout(d time.Duration) BridgeOption {
	return func(b *uiBridge) { b.cleanupTimeout = d }
}

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	renderer            ports.UIRenderer
	logger              *slog.Logger
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
	closeOnce           sync.Once
	cleanupOnce         sync.Once
	cleanupInvocations  int32
	wg                  sync.WaitGroup
	cleanupTimeout      time.Duration
	isPoisoned          bool
}

func (b *uiBridge) stopActiveSpinner() {
	stop := b.stopSpinner
	b.stopSpinner = nil

	if stop != nil {
		// Protect the boundary against double-panics from external UI dependencies
		func() {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Debug("Recovered from panic while stopping spinner", "panic", r)
				}
			}()
			stop()
		}()
	}
}

func (b *uiBridge) resumeActiveSpinner() {
	phase := b.activePhase
	if phase != nil {
		b.startSpinnerForPhase(phase)
	}
}

// CloseInput safely closes the event channel. This MUST be called by the producer
// (Orchestrator) after all sending goroutines have finished.
func (b *uiBridge) CloseInput() {
	b.closeOnce.Do(func() {
		close(b.eventCh)
	})
}

// Cleanup stops any active spinner and waits for events to drain.
// It assumes CloseInput() has already been called.
func (b *uiBridge) Cleanup() {
	b.cleanupOnce.Do(func() {
		atomic.AddInt32(&b.cleanupInvocations, 1)

		// 1. Set up the wait mechanism
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()

		// 2. Wait with timeout
		timer := time.NewTimer(b.cleanupTimeout)
		defer timer.Stop()

		select {
		case <-done:
			// Clean exit: all workers finished draining within the timeout
			b.cancel()
		case <-timer.C:
			// Timeout reached: The renderer might be deadlocked or too slow.
			b.logger.Warn("UI Bridge cleanup timed out, forcing context cancellation")

			// Forcefully unblock the hanging renderer, which unblocks the loop,
			// allowing the background wg.Wait() goroutine to eventually exit.
			b.cancel()
		}
	})
}

// newUIBridge creates a new uiBridge.
func newUIBridge(parentCtx context.Context, renderer ports.UIRenderer, opts ...BridgeOption) *uiBridge {
	ctx, cancel := context.WithCancel(parentCtx)
	b := &uiBridge{
		ctx:            ctx,
		cancel:         cancel,
		renderer:       renderer,
		logger:         slog.Default(),
		eventCh:        make(chan events.Event, 100),
		cleanupTimeout: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	b.wg.Add(1)
	go b.loop()
	return b
}

func (b *uiBridge) loop() {
	defer b.wg.Done()
	for {
		select {
		case <-b.ctx.Done():
			// Forced abort (only happens if UI is deadlocked and Cleanup times out)
			b.stopActiveSpinner()
			return
		case e, ok := <-b.eventCh:
			if !ok {
				// Channel closed by producer (via Cleanup), natural drain complete
				b.stopActiveSpinner()
				return
			}
			b.processRecoverable(e)
		}
	}
}

func (b *uiBridge) processRecoverable(e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			b.isPoisoned = true
			b.logger.Error("uiBridge actor recovered from panic", "error", r)
			b.logger.Debug("uiBridge recovery stack trace", "stack", string(debug.Stack()))
			b.stopActiveSpinner()
			// Trigger shutdown to avoid unpredictable state
			b.cancel()
		}
	}()
	b.processEvent(e)
}

// handleEvent processes a domain event and updates the UI.
func (b *uiBridge) handleEvent(ctx context.Context, e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			// The panic is caught. Log that the event was dropped because the bridge is closed.
			b.logger.Debug("Dropped UI event because bridge input is closed", "panic", r)
		}
	}()

	if b.ctx.Err() != nil {
		return
	}

	switch e.(type) {
	case events.ResponseEvent, events.SystemMessageEvent,
		events.ConsentStartedEvent, events.ConsentFinishedEvent,
		events.TurnStarted, events.TurnStatusEvent,
		events.ToolCallEvent, events.ToolResultEvent,
		events.UsageMetricsEvent:
		// Critical events: ensure delivery and enforce true backpressure.
		select {
		case b.eventCh <- e:
			// Queued successfully
		case <-ctx.Done():
			b.logger.Debug("Caller context cancelled while waiting to queue critical event")
		case <-b.ctx.Done():
			b.logger.Debug("Bridge shutting down, dropping critical event")
		}
	default:
		// Safe to shed visual/transient events if queue is full
		select {
		case b.eventCh <- e:
		case <-ctx.Done():
		case <-b.ctx.Done():
		default:
			b.logger.Debug("UI Bridge queue full, shedding load/visual event")
		}
	}
}

func (b *uiBridge) processEvent(e events.Event) {
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
	b.renderer.LogSystemMessage(b.ctx, msg, lvl)
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
	b.renderer.LogTurnStatus(b.ctx, ev.Status)
}

func (b *uiBridge) handleResponse(ev events.ResponseEvent) {
	b.isRendering = true
	b.activePhase = nil // Clear phase on response
	b.stopActiveSpinner()
	b.renderer.RenderResponse(b.ctx, ev.Content, b.showThoughts, b.rawOutput)
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
		b.renderer.LogToolCall(b.ctx, ev.Calls, ev.Turn, ev.MaxTurns, b.showTools)
		b.resumeActiveSpinner()
	case events.ToolResultEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolResult(b.ctx, ev.Name, ev.Result, b.showTools)
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
		b.renderer.LogSystemMessage(b.ctx, name+" missing context", "warn")
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
	Clock           clock.Clock
	EntropySource   io.Reader
}

// Run is the high-level entry point for running a chat session.
// It simplifies the public API by encapsulating internal component assembly.
func Run(ctx context.Context, params RunParams) error {
	clk := params.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	entropy := params.EntropySource
	if entropy == nil {
		entropy = rand.Reader
	}

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
		clk,
		entropy,
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

	isTTY := params.Capturer.IsTTY(params.Stdout)

	// Phase 1: Render History (if requested)
	if sCfg.GetLastN() > 0 {
		orch.RenderHistory(params.Deps.GetHistoryManager(), sCfg, isTTY)
	}

	// Phase 2: Handle Rollback (if requested)
	if sCfg.GetBackN() > 0 {
		if err := orch.Rollback(ctx, sCfg, params.Deps); err != nil {
			return err
		}
	}

	// Phase 3: Main Orchestration Loop (Chat)
	// Execute chat only if a prompt is provided. This removes CLI-specific early exits.
	if sCfg.GetPrompt() != "" {
		return orch.Run(ctx, sCfg, params.Deps, params.Capturer)
	}

	return nil
}
