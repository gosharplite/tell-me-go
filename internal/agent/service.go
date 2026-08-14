// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/ioutils"
)

type chatService struct {
	HomeDir string
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	SM      domain_security.Manager

	LifecycleManager ports.SessionLifecycleManager
	ChatterFactory   ports.ChatterFactory
	UIRenderer       ports.UIRenderer
	HistoryRenderer  ports.HistoryRenderer
	HistoryBrowser   ports.HistoryBrowser
	HistoryEditor    ports.HistoryEditor
	LogOpener        ports.LogFileOpener

	// resolvePaths resolves session filesystem paths. Defaults to persistence.ResolvePaths.
	// Injectable for testing the empty-path defensive guard in StreamTurnsLog.
	resolvePaths func(homeDir, mode string) *persistence.Paths
}

// NewChatService creates a new concrete implementation of ports.ChatService with explicit dependency injection.
func NewChatService(cfg ports.ChatServiceConfig) ports.ChatService {
	if cfg.ResolvePaths == nil {
		cfg.ResolvePaths = persistence.ResolvePaths
	}
	return &chatService{
		HomeDir:          cfg.HomeDir,
		Version:          cfg.Version,
		Stdout:           cfg.Stdout,
		Stderr:           cfg.Stderr,
		SM:               cfg.SM,
		LifecycleManager: cfg.LifecycleManager,
		ChatterFactory:   cfg.ChatterFactory,
		UIRenderer:       cfg.UIRenderer,
		HistoryRenderer:  cfg.HistoryRenderer,
		HistoryBrowser:   cfg.HistoryBrowser,
		HistoryEditor:    cfg.HistoryEditor,
		LogOpener:        cfg.LogOpener,
		resolvePaths:     cfg.ResolvePaths,
	}
}

// GetLastUserMessage implements ports.ChatService.
func (s *chatService) GetLastUserMessage(ctx context.Context, hManager ports.HistoryManager) (string, int, error) {
	msg, turns, err := hManager.GetLastUserMessage(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get last user message: %w", err)
	}
	return msg, turns, nil
}

// handleRetryConfirmation encapsulates the user-facing retry orchestration logic.
func (s *chatService) handleRetryConfirmation(ctx context.Context, hManager ports.HistoryManager, cmd *ports.ChatCommand, capturer ports.CapturerInteractor) (bool, error) {
	if !cmd.Retry {
		return true, nil
	}

	lastMsg, turns, err := s.GetLastUserMessage(ctx, hManager)
	if err != nil {
		return false, fmt.Errorf("failed to get last user message for retry: %w", err)
	}
	if lastMsg == "" {
		return false, errors.New("no previous user message found to retry")
	}

	confirmMsg := fmt.Sprintf("Are you sure you want to retry the following message?\n\n%s\n\nRetry? [y/N]: ", lastMsg)
	confirmed, err := capturer.Confirm(ctx, confirmMsg)
	if err != nil {
		return false, err
	}
	if !confirmed {
		return false, nil // User aborted
	}

	cmd.Prompt = lastMsg
	cmd.BackN = turns
	return true, nil
}

// cleanupSession encapsulates the logic for shutting down session resources.
func (s *chatService) cleanupSession(deps ports.ChatterComposer, cleanup func(context.Context) error) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ports.DefaultShutdownTimeout)
	defer cancel()

	if err := cleanup(shutdownCtx); err != nil {
		_, _ = fmt.Fprintf(s.Stderr, "Warning: Session cleanup failed: %v\n", err)
	}

	if err := deps.GetEventBus().Shutdown(shutdownCtx); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			_, _ = fmt.Fprintf(s.Stderr, "Warning: Event bus shutdown failed: %v\n", err)
		}
	}
}

// ProcessMessage implements ports.ChatService.
func (s *chatService) ProcessMessage(ctx context.Context, cfg *domain_config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
	// Apply the configured word-wrap width to the markdown renderer before any output.
	s.UIRenderer.SetWordWrap(cfg.WrapWidth)

	// 1. Build session dependencies
	deps, hManager, cleanup, err := s.LifecycleManager.BuildSessionDependencies(ctx, cfg, cmd.ConfigPath, cmd.NewSession, capturer)
	if err != nil {
		return err
	}

	defer s.cleanupSession(deps, cleanup)

	// 2. Handle Retry Orchestration
	proceed, err := s.handleRetryConfirmation(ctx, hManager, &cmd, capturer)
	if err != nil || !proceed {
		return err
	}

	// 3. Delegate to agent orchestration
	err = session.Run(ctx, session.RunParams{
		HomeDir:          s.HomeDir,
		Version:          s.Version,
		SM:               s.SM,
		Stdout:           s.Stdout,
		Stderr:           s.Stderr,
		AgentFactory:     s.ChatterFactory,
		HistoryRenderer:  s.HistoryRenderer,
		UIRenderer:       s.UIRenderer,
		ConfigPath:       cmd.ConfigPath,
		NewSession:       cmd.NewSession,
		LastN:            cmd.LastN,
		BackN:            cmd.BackN,
		RawOutput:        cmd.RawOutput,
		TUIOutput:        cmd.TUIOutput,
		ProgressRenderer: cmd.ProgressRenderer,
		Prompt:           cmd.Prompt,
		Config:           cfg,
		Deps:             deps,
		Capturer:         capturer,
	})

	// 4. Finalize session state
	return s.finalizeSessionState(ctx, hManager, deps, cfg, err)
}

// finalizeSessionState handles the terminal session transitions and error aggregation.
func (s *chatService) finalizeSessionState(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, cfg *domain_config.Config, runErr error) error {
	finalizeErr := s.LifecycleManager.FinalizeSession(ctx, hManager, deps, cfg)
	if finalizeErr == nil {
		return runErr
	}

	if runErr == nil {
		return fmt.Errorf("finalize session failed: %w", finalizeErr)
	}

	// Use errors.Join to allow errors.Is/As to work on both error chains
	return errors.Join(
		fmt.Errorf("session processing failed: %w", runErr),
		fmt.Errorf("finalize session failed: %w", finalizeErr),
	)
}

// BrowseHistory initializes the TUI history browser and runs the Bubble Tea loop.
func (s *chatService) BrowseHistory(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	return s.HistoryBrowser.Browse(ctx, provider, hManager)
}

// EditLastTurn launches the editor TUI for the last model turn.
func (s *chatService) EditLastTurn(ctx context.Context, hManager ports.HistoryManager) error {
	return s.HistoryEditor.Edit(ctx, hManager)
}

// UpdateLastTurn replaces the text of the last model turn, or deletes
// the turn entirely when text is empty (useful for refusal recovery).
func (s *chatService) UpdateLastTurn(ctx context.Context, hManager ports.HistoryManager, text string) error {
	if text == "" {
		// Delete the last model turn by rolling back one turn.
		_, _, _, err := hManager.RollbackTurns(ctx, 1)
		if err != nil {
			return fmt.Errorf("update last turn (delete): %w", err)
		}
		return nil
	}

	idx, _, err := hManager.GetLastModelTurn(ctx)
	if err != nil {
		return fmt.Errorf("update last turn: %w", err)
	}

	if err := hManager.UpdateTurnContent(ctx, idx, text, ""); err != nil {
		return fmt.Errorf("update last turn: %w", err)
	}

	return nil
}

// GetToolNames retrieves the names of all available tools.
func (s *chatService) GetToolNames(ctx context.Context, reg tools.Registry) ([]string, error) {
	declarations := reg.GetDeclarations()
	names := make([]string, 0, len(declarations))
	for _, d := range declarations {
		names = append(names, d.Name)
	}
	return names, nil
}

// StreamTurnsLog resolves the turns log path for the current mode and streams it to the provided writer.
func (s *chatService) StreamTurnsLog(ctx context.Context, cfg *domain_config.Config, out io.Writer) (err error) {
	paths := s.resolvePaths(s.HomeDir, cfg.Mode)
	// Coverage: defensive guard — the default resolvePaths (persistence.ResolvePaths)
	// always produces a non-empty TurnsLogPath for all valid modes (empty/invalid modes
	// fall back to "default"). This guard is a safety net against future regressions in
	// path resolution. See TestChatService_StreamTurnsLog_EmptyPath for verification
	// that a nil/empty TurnsLogPath returns "turns log path not available".
	if paths.TurnsLogPath == "" {
		return errors.New("turns log path not available")
	}

	file, err := s.LogOpener.Open(ctx, paths.TurnsLogPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(out, "No turns log found for this session yet.")
			return nil
		}
		return fmt.Errorf("failed to open turns log at %s: %w", paths.TurnsLogPath, err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close turns log: %w", cerr)
		}
	}()

	ctxReader := ioutils.NewContextReader(ctx, file)
	if _, err := io.Copy(out, ctxReader); err != nil {
		return fmt.Errorf("failed to stream log: %w", err)
	}
	return nil
}

// RunDiagnostics implements ports.ChatService.
func (s *chatService) RunDiagnostics(ctx context.Context, cfg *domain_config.Config, configPath string, jsonOutput bool) error {
	// Apply the configured word-wrap width to the markdown renderer before any output.
	s.UIRenderer.SetWordWrap(cfg.WrapWidth)

	// 1. Build session dependencies
	deps, _, cleanup, err := s.LifecycleManager.BuildSessionDependencies(ctx, cfg, configPath, false, nil)
	if err != nil {
		return err
	}

	defer s.cleanupSession(deps, cleanup)

	// 2. Perform health check
	type healthChecker interface {
		GetHealthManager() ports.HealthCheckManager
	}
	hc, ok := deps.(healthChecker)
	if !ok || hc.GetHealthManager() == nil {
		return errors.New("health check manager not available")
	}

	report, err := hc.GetHealthManager().CheckAll(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	// 3. Render report
	if jsonOutput {
		// Coverage: defensive guard — HealthReport contains only JSON-safe types (string, map, time.Time); marshal failure requires memory corruption.
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize health report: %w", err)
		}
		_, _ = fmt.Fprintln(s.Stdout, string(data))
	} else {
		s.UIRenderer.SetUseColor(s.UIRenderer.IsTerminalContext())
		s.UIRenderer.RenderHealthReport(ctx, report)
	}

	// 4. Handle exit status
	if report.OverallStatus == ports.StatusUnhealthy {
		return errors.New("system health check failed")
	}

	return nil
}
