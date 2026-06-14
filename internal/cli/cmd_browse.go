// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"io"
	"os"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/spf13/cobra"
)

type browseCommand struct {
	ctx              *context
	capturerOverride agent.CapturerInteractor                                                                                                                                                                // test-only injection point
	capturerFactory  func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor // test-only injection; defaults to ui.NewCapturer
}

// newCapturer calls the injected factory or falls back to ui.NewCapturer.
func (c *browseCommand) newCapturer(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
	if c.capturerFactory != nil {
		return c.capturerFactory(stdin, stdout, stderr, sm, clk, mockPrompt, mockAnswer, disableEscapeSequences)
	}
	return ui.NewCapturer(stdin, stdout, stderr, sm, clk, mockPrompt, mockAnswer, disableEscapeSequences)
}

// warnf writes a formatted warning message to stderr; errors from Fprintf are
// deliberately discarded because there is no recovery path for logging failures.
func (c *browseCommand) warnf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(c.ctx.Stderr, format, args...)
}

// getCapturer returns the override if set (test path), otherwise creates a real capturer.
func (c *browseCommand) getCapturer() (agent.CapturerInteractor, func(stdctx.Context) error, error) {
	if c.capturerOverride != nil {
		return c.capturerOverride, func(ctx stdctx.Context) error {
			if err := c.capturerOverride.Close(ctx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}, nil
	}
	return c.setupCapturer()
}

func newBrowseCommand(ctx *context) *cobra.Command {
	c := &browseCommand{ctx: ctx}
	c.capturerFactory = ui.NewCapturer
	cmd := &cobra.Command{
		Use:   "browse",
		Short: "Interactive history browser using Bubble Tea",
		Long:  "Launches an interactive TUI to browse through active and archived chat history.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config") // flag guaranteed by root command; never errors
			return c.runBrowse(cmd.Context(), configPath)
		},
	}

	return cmd
}

// runBrowse launches the interactive history browser.
func (c *browseCommand) runBrowse(ctx stdctx.Context, configPath string) error {
	capturer, cleanup, err := c.getCapturer()
	if err != nil {
		return err
	}
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

func (c *browseCommand) setupCapturer() (agent.CapturerInteractor, func(stdctx.Context) error, error) {
	capturerInterface := c.newCapturer(c.ctx.Stdin, c.ctx.Stdout, c.ctx.Stderr, c.ctx.SM, clock.RealClock{}, c.ctx.MockPrompt, c.ctx.MockAnswer, false)
	capturer, ok := capturerInterface.(agent.CapturerInteractor)
	if !ok {
		return nil, nil, fmt.Errorf("ui.NewCapturer did not return an agent.CapturerInteractor")
	}
	c.ctx.Interactor.set(capturer)
	return capturer, func(ctx stdctx.Context) error {
		if err := capturer.Close(ctx); err != nil {
			c.warnf("Warning: failed to close capturer: %v\n", err)
			return err
		}
		return nil
	}, nil
}
