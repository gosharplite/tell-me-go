// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/spf13/cobra"
)

type browseCommand struct {
	ctx *context
}

func newBrowseCommand(ctx *context) *cobra.Command {
	c := &browseCommand{ctx: ctx}
	cmd := &cobra.Command{
		Use:   "browse",
		Short: "Interactive history browser using Bubble Tea",
		Long:  "Launches an interactive TUI to browse through active and archived chat history.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			return c.runBrowse(cmd.Context(), configPath)
		},
	}

	return cmd
}

// runBrowse launches the interactive history browser.
func (c *browseCommand) runBrowse(ctx stdctx.Context, configPath string) error {
	capturer, cleanup := c.setupCapturer()
	defer func() {
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		_ = cleanup(shutdownCtx)
	}()

	// Bubble Tea requires a TTY for interactive operation.
	if !capturer.IsTTY(os.Stdout) {
		return fmt.Errorf("browse command requires an interactive TTY")
	}

	cfg, err := c.ctx.Loader.Load(configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", configPath, err)
	}

	hManager, err := c.ctx.Bootstrapper.GetHistoryManager(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to get history manager: %w", err)
	}

	provider, err := c.ctx.Bootstrapper.GetUnifiedHistoryProvider(ctx, cfg, hManager)
	if err != nil {
		return fmt.Errorf("failed to get unified history provider: %w", err)
	}

	return c.ctx.ChatService.BrowseHistory(ctx, provider, hManager)
}

func (c *browseCommand) setupCapturer() (agent.CapturerInteractor, func(stdctx.Context) error) {
	capturerInterface := ui.NewCapturer(c.ctx.Stdin, c.ctx.Stdout, c.ctx.Stderr, c.ctx.SM, clock.RealClock{}, c.ctx.MockPrompt, c.ctx.MockAnswer, false)
	capturer, ok := capturerInterface.(agent.CapturerInteractor)
	if !ok {
		return nil, func(stdctx.Context) error { return nil }
	}
	c.ctx.Interactor.set(capturer)
	return capturer, func(ctx stdctx.Context) error {
		return capturer.Close(ctx)
	}
}
