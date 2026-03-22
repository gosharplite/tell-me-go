// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/di"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

type chatService struct {
	HomeDir   string
	Version   string
	Stdout    io.Writer
	Stderr    io.Writer
	SM        domain_security.Manager
	Loader    domain_config.ConfigLoader
	Container di.Container
}

// NewChatService creates a new concrete implementation of ChatService.
func NewChatService(homeDir, version string, stdout, stderr io.Writer, sm domain_security.Manager, loader domain_config.ConfigLoader, container di.Container) ChatService {
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

// ProcessMessage implements ChatService.
func (s *chatService) ProcessMessage(ctx context.Context, opts ChatOptions, capturer orchestration.Capturer) error {
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

	if opts.Retry {
		total := hManager.GetTotalEntries()
		window, err := hManager.GetWindow(ctx, 0, total)
		if err != nil {
			return fmt.Errorf("failed to retrieve history for retry: %w", err)
		}

		var lastMsgText string
		var humanTurnIndex int
		
		// Reverse search the history window
		for i := len(window) - 1; i >= 0; i-- {
			if window[i].Role == "user" {
				// We must find a user message that has actual text (not just tool responses)
				var textBuilder string
				for _, part := range window[i].Parts {
					if part.Text != "" {
						textBuilder += part.Text
					}
				}
				
				if textBuilder != "" {
					lastMsgText = textBuilder
					// Calculate how many messages to rollback.
					// If we are at index i, the number of messages after it is len(window) - i
					// We want to rollback everything AFTER this message, AND this message itself.
					// Since rollback is in "turns" (pairs of 2), we calculate based on the index.
					humanTurnIndex = i / 2 
					break
				}
			}
		}

		if lastMsgText == "" {
			return errors.New("no previous user message found to retry")
		}

		confirmed, err := capturer.(domain_security.UserInteractor).Confirm(ctx, fmt.Sprintf("Retry last message: %q?", lastMsgText))
		if err != nil {
			return fmt.Errorf("failed to prompt for retry confirmation: %w", err)
		}
		if !confirmed {
			return nil
		}

		totalTurns := total / 2
		turnsToRollback := totalTurns - humanTurnIndex

		if turnsToRollback > 0 {
			if _, _, _, err := hManager.RollbackTurns(ctx, turnsToRollback); err != nil {
				return fmt.Errorf("failed to rollback history: %w", err)
			}
		}

		opts.Prompt = lastMsgText
	}

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
