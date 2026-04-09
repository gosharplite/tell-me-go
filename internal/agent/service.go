// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
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

// SessionLifecycleManager defines the interface for building and finalizing sessions.
type SessionLifecycleManager interface {
	BuildSessionDependencies(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(context.Context) error, error)
	FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *domain_config.Config) error
}

// LogFileOpener defines the minimal interface required to open session log files.
type LogFileOpener interface {
	Open(ctx context.Context, name string) (persistence.File, error)
}

type chatService struct {
	HomeDir string
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	SM      domain_security.Manager

	LifecycleManager SessionLifecycleManager
	ChatterFactory   ports.ChatterFactory
	UIRenderer       ports.UIRenderer
	HistoryRenderer  ports.HistoryRenderer
	HistoryBrowser   ports.HistoryBrowser
	LogOpener        LogFileOpener
}

// NewChatService creates a new concrete implementation of ChatService with explicit dependency injection.
func NewChatService(
	homeDir, version string,
	stdout, stderr io.Writer,
	sm domain_security.Manager,
	lifecycleManager SessionLifecycleManager,
	chatterFactory ports.ChatterFactory,
	uiRenderer ports.UIRenderer,
	historyRenderer ports.HistoryRenderer,
	historyBrowser ports.HistoryBrowser,
	logOpener LogFileOpener,
) ChatService {
	return &chatService{
		HomeDir:          homeDir,
		Version:          version,
		Stdout:           stdout,
		Stderr:           stderr,
		SM:               sm,
		LifecycleManager: lifecycleManager,
		ChatterFactory:   chatterFactory,
		UIRenderer:       uiRenderer,
		HistoryRenderer:  historyRenderer,
		HistoryBrowser:   historyBrowser,
		LogOpener:        logOpener,
	}
}

// GetLastUserMessage implements ChatService.
func (s *chatService) GetLastUserMessage(ctx context.Context, hManager ports.HistoryManager) (string, int, error) {
	msg, turns, err := hManager.GetLastUserMessage(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get last user message: %w", err)
	}
	return msg, turns, nil
}

// handleRetryConfirmation encapsulates the user-facing retry orchestration logic.
func (s *chatService) handleRetryConfirmation(ctx context.Context, hManager ports.HistoryManager, cmd *ChatCommand, capturer CapturerInteractor) (bool, error) {
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
func (s *chatService) cleanupSession(deps ports.SessionDependencies, cleanup func(context.Context) error) {
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

// ProcessMessage implements ChatService.
func (s *chatService) ProcessMessage(ctx context.Context, cfg *domain_config.Config, cmd ChatCommand, capturer CapturerInteractor) error {
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
		HomeDir:         s.HomeDir,
		Version:         s.Version,
		SM:              s.SM,
		Stdout:          s.Stdout,
		Stderr:          s.Stderr,
		AgentFactory:    s.ChatterFactory,
		HistoryRenderer: s.HistoryRenderer,
		UIRenderer:      s.UIRenderer,
		ConfigPath:      cmd.ConfigPath,
		NewSession:      cmd.NewSession,
		LastN:           cmd.LastN,
		BackN:           cmd.BackN,
		RawOutput:       cmd.RawOutput,
		Prompt:          cmd.Prompt,
		Config:          cfg,
		Deps:            deps,
		Capturer:        capturer,
	})

	// 4. Finalize session state
	return s.finalizeSessionState(ctx, hManager, deps, cfg, err)
}

// finalizeSessionState handles the terminal session transitions and error aggregation.
func (s *chatService) finalizeSessionState(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *domain_config.Config, runErr error) error {
	if finalizeErr := s.LifecycleManager.FinalizeSession(ctx, hManager, deps, cfg); finalizeErr != nil {
		if runErr != nil {
			return fmt.Errorf("session processing failed: %w; additionally, finalize session failed: %w", runErr, finalizeErr)
		}
		return fmt.Errorf("finalize session failed: %w", finalizeErr)
	}

	return runErr
}

// BrowseHistory initializes the TUI history browser and runs the Bubble Tea loop.
func (s *chatService) BrowseHistory(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	return s.HistoryBrowser.Browse(ctx, provider, hManager)
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
	paths := persistence.ResolvePaths(s.HomeDir, cfg.Mode)
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
