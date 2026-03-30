// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui"
)

// Container defines the interface for building session dependencies and provides factories.
// This interface is defined here to break the import cycle with internal/infrastructure/di.
type Container interface {
	BuildSessionDependencies(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer domain_security.UserInteractor) (ports.SessionDependencies, ports.HistoryManager, func(), error)
	GetAgentFactory() ports.ChatterFactory
	FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *domain_config.Config) error
	GetHistoryManager(ctx context.Context, cfg *domain_config.Config) (ports.HistoryManager, error)
	GetUnifiedHistoryProvider(ctx context.Context, cfg *domain_config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error)
	GetToolNames(ctx context.Context, cfg *domain_config.Config, configPath string) ([]string, error)
	GetSuggestionService(ctx context.Context, recentHistory []string) (ports.SuggestionService, error)
}

type chatService struct {
	HomeDir   string
	Version   string
	Stdout    io.Writer
	Stderr    io.Writer
	SM        domain_security.Manager
	Loader    domain_config.ConfigLoader
	Container Container
}

// NewChatService creates a new concrete implementation of ChatService.
func NewChatService(homeDir, version string, stdout, stderr io.Writer, sm domain_security.Manager, loader domain_config.ConfigLoader, container Container) ChatService {
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
func (s *chatService) ProcessMessage(ctx context.Context, opts ChatOptions, capturer ports.Capturer) error {
	// 1. Load configuration
	cfg, err := s.Loader.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.ConfigPath, err)
	}

	// 2. Build session dependencies
	deps, hManager, cleanup, err := s.Container.BuildSessionDependencies(ctx, cfg, opts.ConfigPath, opts.NewSession, capturer.(domain_security.UserInteractor))
	if err != nil {
		return err
	}
	defer cleanup()

	defer func() {
		if err := deps.GetEventBus().Shutdown(ctx); err != nil {
			if errors.Is(err, events.ErrBusNotInitialized) {
				return
			}
			_, _ = fmt.Fprintf(s.Stderr, "Warning: Event bus shutdown failed: %v\n", err)
		}
	}()

	// 3. Delegate to agent orchestration
	uiRenderer := ui.NewRenderer(s.SM, s.Stdout, s.Stderr, clock.RealClock{})
	historyRenderer := &ui.StdHistoryRenderer{}

	err = orchestration.Run(ctx, orchestration.RunParams{
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
func (s *chatService) BrowseHistory(ctx context.Context, configPath string, capturer ports.Capturer) error {
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

	// Initialize background logger for TUI
	if closer, err := tui.InitLogger(); err == nil {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				log.Printf("failed to close tui logger: %v", closeErr)
			}
		}()
	}

	// TTY check is done in the command layer or here.
	// We'll trust the caller for now, or check if capturer is available.

	model := tui.NewRootBrowserModel(ctx, provider, hManager)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui program error: %w", err)
	}

	return nil
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
