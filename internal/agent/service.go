// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
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
	SM        domain_security.ISecurityManager
	Loader    domain_config.ConfigLoader
	Container di.Container
}

// NewChatService creates a new concrete implementation of ChatService.
func NewChatService(homeDir, version string, stdout, stderr io.Writer, sm domain_security.ISecurityManager, loader domain_config.ConfigLoader, container di.Container) ChatService {
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

	defer func() {
		if shutdownErr := deps.GetEventBus().Shutdown(ctx); shutdownErr != nil {
			_, _ = fmt.Fprintf(s.Stderr, "Warning: Event bus shutdown failed: %v\n", shutdownErr)
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
			return fmt.Errorf("session processing failed: %w; additionally, finalize session failed: %v", err, finalizeErr)
		}
		return fmt.Errorf("finalize session failed: %w", finalizeErr)
	}

	return err
}
