// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// Container defines the interface for building session dependencies and provides factories.
// This interface is defined here to break the import cycle with internal/infrastructure/di.


type chatService struct {
	HomeDir   string
	Version   string
	Stdout    io.Writer
	Stderr    io.Writer
	SM        domain_security.Manager
	Loader    domain_config.ConfigLoader
	Container ports.Container
}

// NewChatService creates a new concrete implementation of ChatService.
func NewChatService(homeDir, version string, stdout, stderr io.Writer, sm domain_security.Manager, loader domain_config.ConfigLoader, container ports.Container) ChatService {
	return &chatService{
		HomeDir:   homeDir,
		Version:   version,
		Stdout:    stdout,
		Stderr:    stderr,
		SM:        sm,
		Loader:    loader,
		Container: container,
	}
}

// GetLastUserMessage implements ChatService.
func (s *chatService) GetLastUserMessage(ctx context.Context, configPath string) (string, int, error) {
	cfg, err := s.Loader.Load(configPath)
	if err != nil {
		return "", 0, fmt.Errorf("error loading config [%s]: %w", configPath, err)
	}

	hManager, err := s.Container.GetHistoryManager(ctx, cfg)
	if err != nil {
		return "", 0, fmt.Errorf("failed to load history manager: %w", err)
	}

	msg, turns, err := hManager.GetLastUserMessage(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get last user message: %w", err)
	}
	return msg, turns, nil
}

// ProcessMessage implements ChatService.
func (s *chatService) ProcessMessage(ctx context.Context, opts ChatOptions, capturer CapturerInteractor) error {
	// 1. Load configuration
	cfg, err := s.Loader.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.ConfigPath, err)
	}

	// 2. Build session dependencies

	deps, hManager, cleanup, err := s.Container.BuildSessionDependencies(ctx, cfg, opts.ConfigPath, opts.NewSession, capturer)
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

	// 3. Delegate to agent orchestration
	uiRenderer := s.Container.GetUIRenderer()
	historyRenderer := s.Container.GetHistoryRenderer()

	err = session.Run(ctx, session.RunParams{
		HomeDir:         s.HomeDir,
		Version:         s.Version,
		Loader:          s.Loader,
		SM:              s.SM,
		Stdout:          s.Stdout,
		Stderr:          s.Stderr,
		AgentFactory:    s.Container.GetAgentFactory(),
		HistoryRenderer: historyRenderer,
		UIRenderer:      uiRenderer,
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

	// 4. Finalize session state
	if finalizeErr := s.Container.FinalizeSession(ctx, hManager, deps, cfg); finalizeErr != nil {
		if err != nil {
			return fmt.Errorf("session processing failed: %w; additionally, finalize session failed: %w", err, finalizeErr)
		}
		return fmt.Errorf("finalize session failed: %w", finalizeErr)
	}

	return err
}

// BrowseHistory initializes the TUI history browser and runs the Bubble Tea loop.
func (s *chatService) BrowseHistory(ctx context.Context, configPath string, capturer CapturerInteractor) error {
	cfg, err := s.Loader.Load(configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", configPath, err)
	}

	hManager, err := s.Container.GetHistoryManager(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to load history manager: %w", err)
	}

	provider, err := s.Container.GetUnifiedHistoryProvider(ctx, cfg, hManager)
	if err != nil {
		return fmt.Errorf("failed to load unified history provider: %w", err)
	}

	browser := s.Container.GetHistoryBrowser()
	return browser.Browse(ctx, provider, hManager)
}

// GetToolNames retrieves the names of all available tools.
func (s *chatService) GetToolNames(ctx context.Context, configPath string) ([]string, error) {
	cfg, err := s.Loader.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("error loading config [%s]: %w", configPath, err)
	}

	return s.Container.GetToolNames(ctx, cfg, configPath)
}

// GetSuggestionService implements ChatService.
func (s *chatService) GetSuggestionService(ctx context.Context, recentHistory []string) (ports.SuggestionService, error) {
	return s.Container.GetSuggestionService(ctx, recentHistory)
}
