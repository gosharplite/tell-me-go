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
)

// SessionLifecycleManager defines the interface for building and finalizing sessions.
type SessionLifecycleManager interface {
	BuildSessionDependencies(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(context.Context) error, error)
	FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *domain_config.Config) error
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
	FileSystem       persistence.FileSystem
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
	fs persistence.FileSystem,
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
		FileSystem:       fs,
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

// ProcessMessage implements ChatService.
func (s *chatService) ProcessMessage(ctx context.Context, cfg *domain_config.Config, opts ChatOptions, capturer CapturerInteractor) error {
	// 1. Build session dependencies
	deps, hManager, cleanup, err := s.LifecycleManager.BuildSessionDependencies(ctx, cfg, opts.ConfigPath, opts.NewSession, capturer)
	if err != nil {
		return err
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		if err := cleanup(shutdownCtx); err != nil {
			_, _ = fmt.Fprintf(s.Stderr, "Warning: Session cleanup failed: %v\n", err)
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		if err := deps.GetEventBus().Shutdown(shutdownCtx); err != nil {
			if errors.Is(err, events.ErrBusNotInitialized) {
				return
			}
			_, _ = fmt.Fprintf(s.Stderr, "Warning: Event bus shutdown failed: %v\n", err)
		}
	}()

	// 2. Delegate to agent orchestration
	err = session.Run(ctx, session.RunParams{
		HomeDir:         s.HomeDir,
		Version:         s.Version,
		SM:              s.SM,
		Stdout:          s.Stdout,
		Stderr:          s.Stderr,
		AgentFactory:    s.ChatterFactory,
		HistoryRenderer: s.HistoryRenderer,
		UIRenderer:      s.UIRenderer,
		ConfigPath:      opts.ConfigPath,
		NewSession:      opts.NewSession,
		LastN:           opts.LastN,
		BackN:           opts.BackN,
		RawOutput:       opts.RawOutput,
		Prompt:          opts.Prompt,
		Config:          cfg,
		Deps:            deps,
		Capturer:        capturer,
	})

	// 3. Finalize session state
	if finalizeErr := s.LifecycleManager.FinalizeSession(ctx, hManager, deps, cfg); finalizeErr != nil {
		if err != nil {
			return fmt.Errorf("session processing failed: %w; additionally, finalize session failed: %w", err, finalizeErr)
		}
		return fmt.Errorf("finalize session failed: %w", finalizeErr)
	}

	return err
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
func (s *chatService) StreamTurnsLog(ctx context.Context, cfg *domain_config.Config, out io.Writer) error {
	paths := persistence.ResolvePaths(s.HomeDir, cfg.Mode)
	if paths.TurnsLogPath == "" {
		return errors.New("turns log path not available")
	}

	file, err := s.FileSystem.Open(ctx, paths.TurnsLogPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(out, "No turns log found for this session yet.")
			return nil
		}
		return fmt.Errorf("failed to open turns log at %s: %w", paths.TurnsLogPath, err)
	}
	defer file.Close()

	if _, err := io.Copy(out, file); err != nil {
		return fmt.Errorf("failed to output turns log: %w", err)
	}
	return nil
}
