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
	SessionProvider  ports.SessionProvider
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
func (d *sessionDependencies) GetSessionProvider() ports.SessionProvider {
	return d.SessionProvider
}
func (d *sessionDependencies) GetPricingOverrides() map[string]domain_pricing.ModelPricing {
	return d.PricingOverrides
}
func (d *sessionDependencies) GetTracker() domain_pricing.CostTracker { return d.Tracker }
func (d *sessionDependencies) GetPricingData() domain_pricing.PricingData {
	return d.PricingData
}

// newSessionDependencies creates a new sessionDependencies with all required components.
func newSessionDependencies(paths *persistence.Paths, hManager ports.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.Registry, sm domain_security.Manager, tracker domain_pricing.CostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus, logger *slog.Logger, sessionProvider ports.SessionProvider) ports.SessionDependencies {
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
		SessionProvider:  sessionProvider,
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
	bridge := newUIBridge(o.UIRenderer,
		withBridgeThoughts(cfg.ShowThoughts),
		withBridgeTools(cfg.ShowTools),
		withBridgeRawOutput(rawOutput),
		withBridgeColor(useColor),
		withBridgeLogFile(logPath),
		withBridgeLogger(logger),
	)
	bridge.Start(ctx)
	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
		_ = bridge.handleEvent(ctx, e)
	})
	return bridge
}

// bridgeOption configures a uiBridge instance.
type bridgeOption func(*uiBridge)

// withBridgeThoughts enables or disables thought rendering.
func withBridgeThoughts(show bool) bridgeOption {
	return func(b *uiBridge) { b.showThoughts = show }
}

// withBridgeTools enables or disables tool call rendering.
func withBridgeTools(show bool) bridgeOption {
	return func(b *uiBridge) { b.showTools = show }
}

// withBridgeRawOutput enables or disables raw output mode.
func withBridgeRawOutput(raw bool) bridgeOption {
	return func(b *uiBridge) { b.rawOutput = raw }
}

// withBridgeColor enables or disables ANSI color support.
func withBridgeColor(color bool) bridgeOption {
	return func(b *uiBridge) { b.useColor = color }
}

// withBridgeLogFile sets the file path for logging usage metrics.
func withBridgeLogFile(path string) bridgeOption {
	return func(b *uiBridge) { b.logFile = path }
}

// withBridgeLogger sets the structured logger.
func withBridgeLogger(l *slog.Logger) bridgeOption {
	return func(b *uiBridge) { b.logger = l }
}

// withBridgeCleanupTimeout sets the duration to wait for the bridge to drain events during cleanup.
func withBridgeCleanupTimeout(d time.Duration) bridgeOption {
	return func(b *uiBridge) { b.cleanupTimeout = d }
}

// UIState represents the possible states of the UI bridge.
type UIState int

const (
	// StateIdle indicates the UI is not performing any active task.
	StateIdle UIState = iota
	// StateThinking indicates the UI is showing a progress indicator (spinner).
	StateThinking
	// StateRendering indicates the UI is rendering a streaming response.
	StateRendering
	// StateAwaitingConsent indicates the UI is waiting for user consent.
	StateAwaitingConsent
)

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	cancel         context.CancelFunc
	renderer       ports.UIRenderer
	logger         *slog.Logger
	showThoughts   bool
	showTools      bool
	rawOutput      bool
	useColor       bool
	logFile        string
	state          UIState
	stopSpinner    func()
	activePhase    events.Event
	eventCh        chan events.Event
	closeOnce      sync.Once
	cleanupOnce    sync.Once
	cleanupInvocations int32
	wg             sync.WaitGroup
	cleanupTimeout time.Duration
	isPoisoned     bool
	isClosed       atomic.Bool
}

func (b *uiBridge) transition(next UIState) {
	if b.state == next {
		return
	}

	// Side effects for entering the new state
	switch next {
	case StateIdle, StateRendering, StateAwaitingConsent:
		b.stopActiveSpinner()
	case StateThinking:
		// StateThinking side effects are typically handled via transitionSpinner
		// but we ensure old spinner is stopped if we were in another state.
		// If we were already in StateThinking, we don't stop here to avoid flicker
		// unless transitionSpinner is called.
		if b.state != StateThinking {
			b.stopActiveSpinner()
		}
	}

	b.state = next
}

func (b *uiBridge) is(state UIState) bool {
	return b.state == state
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

func (b *uiBridge) resumeActiveSpinner(ctx context.Context) {
	phase := b.activePhase
	if phase != nil {
		b.startSpinnerForPhase(ctx, phase)
	}
}

// CloseInput safely closes the event channel. This MUST be called by the producer
// (Orchestrator) after all sending goroutines have finished.
func (b *uiBridge) CloseInput() {
	b.closeOnce.Do(func() {
		b.isClosed.Store(true)
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
			if b.cancel != nil {
				b.cancel()
			}
		case <-timer.C:
			// Timeout reached: The renderer might be deadlocked or too slow.
			b.logger.Warn("UI Bridge cleanup timed out, forcing context cancellation")

			// Forcefully unblock the hanging renderer, which unblocks the loop,
			// allowing the background wg.Wait() goroutine to eventually exit.
			if b.cancel != nil {
				b.cancel()
			}
		}
	})
}

// newUIBridge creates a new uiBridge.
func newUIBridge(renderer ports.UIRenderer, opts ...bridgeOption) *uiBridge {
	b := &uiBridge{
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
	return b
}

func (b *uiBridge) Start(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.wg.Add(1)
	go b.loop(ctx)
	return ctx
}

func (b *uiBridge) loop(ctx context.Context) {
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			// Forced abort (only happens if UI is deadlocked and Cleanup times out)
			b.stopActiveSpinner()
			return
		case e, ok := <-b.eventCh:
			if !ok {
				// Channel closed by producer (via Cleanup), natural drain complete
				b.stopActiveSpinner()
				return
			}
			b.processRecoverable(ctx, e)
		}
	}
}

func (b *uiBridge) processRecoverable(ctx context.Context, e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			b.isPoisoned = true
			b.logger.Error("uiBridge actor recovered from panic", "error", r)
			b.logger.Debug("uiBridge recovery stack trace", "stack", string(debug.Stack()))
			b.stopActiveSpinner()
			// Trigger shutdown to avoid unpredictable state
			if b.cancel != nil {
				b.cancel()
			}
		}
	}()
	b.processEvent(ctx, e)
}

// handleEvent processes a domain event and updates the UI.
func (b *uiBridge) handleEvent(ctx context.Context, e events.Event) error {
	if b.isClosed.Load() {
		b.logger.Debug("Shedding event: bridge is closed")
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			// Log as Warn/Error since it's now unexpected (last-resort safety)
			b.logger.Warn("Unexpected panic in uiBridge.handleEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return b.enqueueEvent(ctx, e)
}

func (b *uiBridge) enqueueEvent(ctx context.Context, e events.Event) error {
	if isCriticalEvent(e) {
		// Critical events: ensure delivery and enforce true backpressure.
		select {
		case b.eventCh <- e:
			return nil
		case <-ctx.Done():
			b.logger.Debug("Caller context cancelled while waiting to queue critical event")
			return ctx.Err()
		}
	}

	// Safe to shed visual/transient events if queue is full
	select {
	case b.eventCh <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		b.logger.Debug("UI Bridge queue full, shedding load/visual event")
		return nil
	}
}

func (b *uiBridge) processEvent(ctx context.Context, e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		b.handleTurnStatus(ctx, ev)
	case events.InferenceStartedEvent, events.SummarizationStartedEvent, events.ToolExecutionStartedEvent, events.RetryWaitingEvent:
		b.handleSpinnerEvent(ctx, ev)
	case events.ConsentStartedEvent:
		b.transition(StateAwaitingConsent)
	case events.ConsentFinishedEvent:
		// Transition back to Idle, which stops any lingering (though should be stopped by ConsentStarted)
		// resumeActiveSpinner will transition to StateThinking if a phase exists.
		b.transition(StateIdle)
		b.resumeActiveSpinner(ctx)
	case events.ResponseEvent:
		b.handleResponse(ctx, ev)
	case events.UsageMetricsEvent:
		b.handleUsageMetrics(ev)
	case events.ToolCallEvent, events.ToolResultEvent:
		b.handleToolEvents(ctx, ev)
	case events.TurnStarted:
		b.handleTurnStarted()
	case events.SystemMessageEvent, events.StatusUpdate:
		b.handleSystemMessage(ctx, ev)
	}
}

func (b *uiBridge) handleSystemMessage(ctx context.Context, e events.Event) {
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
	b.renderer.LogSystemMessage(ctx, msg, lvl)
	b.resumeActiveSpinner(ctx)
}

func (b *uiBridge) handleSpinnerEvent(ctx context.Context, e events.Event) {
	b.activePhase = e
	b.startSpinnerForPhase(ctx, e)
}

func (b *uiBridge) startSpinnerForPhase(ctx context.Context, e events.Event) {
	info, ok := getSpinnerInfo(e)
	if !ok {
		return
	}

	if info.resetRendering && b.is(StateRendering) {
		b.transition(StateIdle)
	}

	b.transitionSpinner(func() func() {
		if info.withMetrics {
			return b.renderer.StartSpinnerWithMetrics(ctx, info.status)
		}
		return b.renderer.StartSpinnerWithStatus(ctx, info.status)
	})
}

func (b *uiBridge) handleTurnStatus(ctx context.Context, ev events.TurnStatusEvent) {
	b.activePhase = nil // Clear phase on new turn/header
	b.transition(StateIdle)
	b.renderer.LogTurnStatus(ctx, ev.Status)
}

func (b *uiBridge) handleResponse(ctx context.Context, ev events.ResponseEvent) {
	b.activePhase = nil // Clear phase on response
	b.transition(StateRendering)
	b.renderer.RenderResponse(ctx, ev.Content, b.showThoughts, b.rawOutput)
}

func (b *uiBridge) handleUsageMetrics(ev events.UsageMetricsEvent) {
	ctx := b.ensureContext(ev.Context, "UsageMetricsEvent")
	b.stopActiveSpinner()
	b.renderer.LogUsage(ctx, ev.Metrics, b.logFile, ev.StartTime)
	b.resumeActiveSpinner(ctx)
}

func (b *uiBridge) handleToolEvents(ctx context.Context, e events.Event) {
	switch ev := e.(type) {
	case events.ToolCallEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolCall(ctx, ev.Calls, ev.Turn, ev.MaxTurns, b.showTools)
		b.resumeActiveSpinner(ctx)
	case events.ToolResultEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolResult(ctx, ev.Name, ev.Result, b.showTools)
		b.resumeActiveSpinner(ctx)
	}
}

func (b *uiBridge) handleTurnStarted() {
	b.activePhase = nil
	b.transition(StateIdle)
}

func (b *uiBridge) transitionSpinner(startFn func() func()) {
	if b.state == StateRendering || b.state == StateAwaitingConsent {
		return
	}

	b.stopActiveSpinner()
	b.stopSpinner = startFn()
	b.state = StateThinking
}

func (b *uiBridge) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		b.logger.Debug(name + " missing context")
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

func isCriticalEvent(e events.Event) bool {
	switch e.(type) {
	case events.ResponseEvent, events.SystemMessageEvent, events.StatusUpdate,
		events.ConsentStartedEvent, events.ConsentFinishedEvent,
		events.TurnStarted, events.TurnStatusEvent,
		events.ToolCallEvent, events.ToolResultEvent,
		events.UsageMetricsEvent:
		return true
	default:
		return false
	}
}

type spinnerInfo struct {
	status         string
	withMetrics    bool
	resetRendering bool
}

func getSpinnerInfo(e events.Event) (spinnerInfo, bool) {
	switch ev := e.(type) {
	case events.InferenceStartedEvent:
		status := " Thinking..."
		if ev.Model != "" {
			status = fmt.Sprintf(" Thinking [%s]...", ev.Model)
		}
		return spinnerInfo{status: status, withMetrics: false, resetRendering: false}, true
	case events.SummarizationStartedEvent:
		return spinnerInfo{status: " Compressing context...", withMetrics: false, resetRendering: true}, true
	case events.ToolExecutionStartedEvent:
		status := " Executing tools..."
		if len(ev.ToolNames) == 1 {
			status = fmt.Sprintf(" Executing [%s]...", ev.ToolNames[0])
		} else if len(ev.ToolNames) > 1 {
			status = fmt.Sprintf(" Executing tools [%s]...", strings.Join(ev.ToolNames, ", "))
		}
		return spinnerInfo{status: status, withMetrics: true, resetRendering: true}, true
	case events.RetryWaitingEvent:
		return spinnerInfo{status: fmt.Sprintf(" Retrying in %v...", ev.Duration.Round(time.Second)), withMetrics: false, resetRendering: true}, true
	default:
		return spinnerInfo{}, false
	}
}
