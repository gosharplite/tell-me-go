// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	register("browse", func(ctx *context) command {
		return newBrowseCommand(ctx)
	})
}

type browseCommand struct {
	ctx *context
}

func newBrowseCommand(ctx *context) *browseCommand {
	return &browseCommand{ctx: ctx}
}

// Execute implements the CLI command interface, wrapping a Cobra command.
func (c *browseCommand) Execute(ctx stdctx.Context, args []string) error {
	cobraCmd := &cobra.Command{
		Use:   "browse",
		Short: "Interactive history browser using Bubble Tea",
		Long:  "Launches an interactive TUI to browse through active and archived chat history.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			return c.runBrowse(ctx, configPath)
		},
	}

	cobraCmd.Flags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")

	// Adjust arguments to bypass the 'browse' command name if called from the central registry
	if len(args) > 0 && args[0] == "browse" {
		cobraCmd.SetArgs(args[1:])
	} else {
		cobraCmd.SetArgs(args)
	}

	return cobraCmd.Execute()
}

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

	return c.ctx.ChatService.BrowseHistory(ctx, configPath, capturer)
}

func (c *browseCommand) setupCapturer() (ports.Capturer, func(stdctx.Context) error) {
	capturer := ui.NewCapturer(c.ctx.Stdin, c.ctx.Stdout, c.ctx.Stderr, c.ctx.SM, clock.RealClock{}, c.ctx.MockPrompt, c.ctx.MockAnswer, false).(ports.Capturer)
	if sm, ok := c.ctx.SM.(interface {
		SetInteractor(domain_security.UserInteractor)
	}); ok {
		sm.SetInteractor(capturer.(domain_security.UserInteractor))
	}
	return capturer, func(stdctx.Context) error { return nil }
}
