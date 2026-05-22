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

	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/sync/errgroup"
)

// sessionManager manages the session lifecycle and agent execution.
type sessionManager struct {
	HomeDir         string
	Version         string
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

// NewSessionConfig creates a new sessionConfig with required parameters.
func NewSessionConfig(configPath string, newSession bool, lastN, backN int, rawOutput bool, prompt string, cfg *config.Config) ports.SessionConfig {
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

// SessionManagerOption defines a functional option for configuring a SessionManager.
type SessionManagerOption func(*sessionManager)

// WithClock sets a custom clock implementation. Defaults to clock.RealClock{}.
func WithClock(c clock.Clock) SessionManagerOption {
	return func(sm *sessionManager) { sm.Clock = c }
}

// WithEntropySource sets a custom entropy source for session ID generation.
// Defaults to rand.Reader.
func WithEntropySource(r io.Reader) SessionManagerOption {
	return func(sm *sessionManager) { sm.EntropySource = r }
}

// NewSessionManager creates a new sessionManager.
func NewSessionManager(homeDir, version string, sm domain_security.Manager, stdout, stderr io.Writer, factory ports.ChatterFactory, historyRenderer ports.HistoryRenderer, uiRenderer ports.UIRenderer, opts ...SessionManagerOption) SessionManager {
	s := &sessionManager{
		HomeDir:         homeDir,
		Version:         version,
		SM:              sm,
		Stdout:          stdout,
		Stderr:          stderr,
		AgentFactory:    factory,
		HistoryRenderer: historyRenderer,
		UIRenderer:      uiRenderer,
		Clock:           clock.RealClock{},
		EntropySource:   rand.Reader,
	}
	for _, o := range opts {
		o(s)
	}
	return s
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
				bridge.AbortStart()
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

func (o *sessionManager) applyConfiguration(ctx context.Context, chatAgent ports.Chatter, sCfg ports.SessionConfig, sd ports.SessionDependencies, capturer ports.Capturer) (*ui.Bridge, error) {
	cfg := sCfg.GetConfig()
	paths := sd.GetPaths()
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
	return bridge, nil
}

func (o *sessionManager) setupUIRendering(ctx context.Context, chatAgent ports.Chatter, cfg *config.Config, rawOutput bool, logPath string, logger ports.Logger, capturer ports.Capturer) *ui.Bridge {
	useColor := capturer.IsTTY(o.Stdout) && !rawOutput
	o.UIRenderer.SetUseColor(useColor)
	bridge := ui.NewBridge(o.UIRenderer,
		ui.WithBridgeThoughts(cfg.ShowThoughts),
		ui.WithBridgeTools(cfg.ShowTools),
		ui.WithBridgeRawOutput(rawOutput),
		ui.WithBridgeColor(useColor),
		ui.WithBridgeLogFile(logPath),
		ui.WithBridgeLogger(logger),
		ui.WithBridgeClock(o.Clock),
	)
	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
		if err := bridge.HandleEvent(ctx, e); err != nil {
			if errors.Is(err, ui.ErrActorDead) {
				logger.Warn("Bridge event failed: actor is dead", "error", err, "event", fmt.Sprintf("%T", e))
			} else if errors.Is(err, context.Canceled) {
				logger.Debug("Bridge event skipped: context cancelled", "event", fmt.Sprintf("%T", e))
			} else {
				logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
			}
		}
	})
	return bridge
}

type RunParams struct {
	HomeDir         string
	Version         string
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
	orch := NewSessionManager(
		params.HomeDir,
		params.Version,
		params.SM,
		params.Stdout,
		params.Stderr,
		params.AgentFactory,
		params.HistoryRenderer,
		params.UIRenderer,
	)

	sCfg := NewSessionConfig(
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
