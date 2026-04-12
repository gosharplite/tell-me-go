// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/sync/errgroup"
)

// sessionManager manages the session lifecycle and agent execution.
type sessionManager struct {
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
	TurnsLogger      ports.TurnsLogger
	SessionProvider  ports.SessionProvider
}

func (d *sessionDependencies) GetGateway() domain_llm.LLMGateway { return d.Gateway }
func (d *sessionDependencies) GetHistoryManager() ports.HistoryManager {
	return d.HistoryManager
}
func (d *sessionDependencies) GetRegistry() (domaintools.Registry, error) { return d.Registry, nil }
func (d *sessionDependencies) GetSecurityManager() domain_security.Manager {
	return d.SecurityManager
}
func (d *sessionDependencies) GetEventBus() events.EventBus { return d.EventBus }
func (d *sessionDependencies) GetLogger() *slog.Logger      { return d.Logger }
func (d *sessionDependencies) GetTurnsLogger() ports.TurnsLogger {
	return d.TurnsLogger
}
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
func newSessionDependencies(paths *persistence.Paths, hManager ports.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.Registry, sm domain_security.Manager, tracker domain_pricing.CostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus, logger *slog.Logger, turnsLogger ports.TurnsLogger, sessionProvider ports.SessionProvider) ports.SessionDependencies {
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
		TurnsLogger:      turnsLogger,
		SessionProvider:  sessionProvider,
	}
}

// newSessionManager creates a new sessionManager.
func newSessionManager(homeDir, version string, loader config.ConfigLoader, sm domain_security.Manager, stdout, stderr io.Writer, factory ports.ChatterFactory, historyRenderer ports.HistoryRenderer, uiRenderer ports.UIRenderer, clk clock.Clock, entropy io.Reader) SessionManager {
	return &sessionManager{
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
func (o *sessionManager) Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic ports.Capturer) (err error) {
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
	listenStarted := false
	// Single defer to guarantee deterministic teardown order:
	// Stop Producers first, then Consumers.
	defer func() {
		// 1. Stop Producers (Agent) first
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ports.DefaultShutdownTimeout)
		defer cancel()

		if se := chatAgent.Shutdown(shutdownCtx); se != nil {
			_, _ = fmt.Fprintf(o.Stderr, "Warning: Agent shutdown failed: %v\n", se)
			// Aggregate with the named return error 'err'
			err = errors.Join(err, fmt.Errorf("agent shutdown failed: %w", se))
		}

		// 2. Clean up Consumer (UI) second
		if bridge != nil {
			if !listenStarted {
				bridge.wg.Done()
			}
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

	// [REFACTOR] Use errgroup to coordinate agent execution and UI rendering background tasks.
	// This ensures that all UI events are processed before the session terminates.
	gCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gCtx := errgroup.WithContext(gCtx)

	// Main Agent Loop
	g.Go(func() error {
		defer cancel()
		return chatAgent.Chat(gCtx, sess, sc.GetPrompt())
	})

	// Background UI Loop
	if bridge != nil {
		listenStarted = true
		g.Go(func() error {
			return bridge.Listen(gCtx)
		})
	}

	return g.Wait()
}

// Rollback deletes the specified number of turns from history.
func (o *sessionManager) Rollback(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies) error {
	if sc.GetBackN() <= 0 {
		return nil
	}

	hManager := sd.GetHistoryManager()
	actualRemoved, remainingTurns, remainingMsgs, err := hManager.RollbackTurns(ctx, sc.GetBackN())
	if err != nil {
		return fmt.Errorf("failed to rollback history: %w", err)
	}

	// Force Sync to ensure Windows file system reconciliation
	_ = hManager.Sync(ctx)

	_, _ = fmt.Fprintf(o.Stdout, "⏪ Rolled back %d turns. History now contains %d turns (%d messages).\n",
		actualRemoved, remainingTurns, remainingMsgs)

	return nil
}

// RenderHistory renders the last N messages from history.
func (o *sessionManager) RenderHistory(hManager ports.HistoryManager, sCfg ports.SessionConfig, isTTY bool) {
	cfg := sCfg.GetConfig()
	o.HistoryRenderer.Render(o.Stdout, hManager, sCfg.GetLastN(), ports.HistoryRenderOptions{
		Raw:          sCfg.GetRawOutput(),
		ShowThoughts: cfg.ShowThoughts,
		UseColor:     isTTY && !sCfg.GetRawOutput(),
	})
}

func (o *sessionManager) applyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, sd ports.SessionDependencies, capturer ports.Capturer) (*uiBridge, error) {
	cfg := sCfg.GetConfig()
	paths := sd.GetPaths()
	pData := sd.GetPricingData()
	logger := sd.GetLogger()
	turnsLogger := sd.GetTurnsLogger()

	if turnsLogger != nil {
		chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
			turnsLogger.HandleEvent(ctx, e)
		})
	}

	bridge := o.setupUIRendering(ctx, chatAgent, cfg, sCfg.GetRawOutput(), paths.LogPath, logger, capturer)
	if err := chatAgent.SetLimits(ctx, cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns); err != nil {
		return bridge, err
	}
	return bridge, chatAgent.SetTieredThreshold(ctx, cfg.ResolveTieredThreshold(pData))
}

func (o *sessionManager) setupUIRendering(ctx context.Context, chatAgent ports.Chatter, cfg *config.Config, rawOutput bool, logPath string, logger *slog.Logger, capturer ports.Capturer) *uiBridge {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := newUIBridge(o.UIRenderer,
		withBridgeThoughts(cfg.ShowThoughts),
		withBridgeTools(cfg.ShowTools),
		withBridgeRawOutput(rawOutput),
		withBridgeColor(useColor),
		withBridgeLogFile(logPath),
		withBridgeLogger(logger),
		withBridgeClock(o.Clock),
	)
	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
		if err := bridge.handleEvent(ctx, e); err != nil {
			logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
		}
	})
	return bridge
}

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

	orch := newSessionManager(
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
