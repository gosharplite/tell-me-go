// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// chatCommand implements the main chat command.
type chatCommand struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	SM           domain_security.Manager
	ChatService  agent.ChatService
	Bootstrapper Bootstrapper
	Loader       domain_config.ConfigLoader
	HomeDir      string
	MockPrompt   string
	MockAnswer   string
}

type cliOptions struct {
	configPath   string
	newSession   bool
	showTurnsLog bool
	lastN        int
	backN        int
	rawOutput    bool
	tuiPrompt    bool
	retry        bool
}

func addChatFlags(fs *pflag.FlagSet, opts *cliOptions) {
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVarP(&opts.showTurnsLog, "turns", "t", false, "Print the contents of the current session's turns.log and exit")
	fs.IntVarP(&opts.lastN, "last", "l", 0, "Show the last N messages from history")
	fs.Lookup("last").NoOptDefVal = "1"
	fs.IntVarP(&opts.backN, "back", "b", 0, "Go back / delete the last N turns from history")
	fs.Lookup("back").NoOptDefVal = "1"
	fs.BoolVarP(&opts.rawOutput, "raw", "r", false, "Show raw output (without markdown rendering)")
	fs.BoolVarP(&opts.tuiPrompt, "interactive", "i", false, "Enable interactive TUI prompt with suggestions")
	fs.BoolVar(&opts.retry, "retry", false, "Retry the last user message")
}

// newChatCommand creates a new Chat Command as a Cobra command.
func newChatCommand(ctx *context, opts *cliOptions) *cobra.Command {
	c := &chatCommand{
		Version:      ctx.Version,
		Stdin:        ctx.Stdin,
		Stdout:       ctx.Stdout,
		Stderr:       ctx.Stderr,
		SM:           ctx.SM,
		ChatService:  ctx.ChatService,
		Bootstrapper: ctx.Bootstrapper,
		Loader:       ctx.Loader,
		HomeDir:      ctx.HomeDir,
		MockPrompt:   ctx.MockPrompt,
		MockAnswer:   ctx.MockAnswer,
	}

	if opts == nil {
		opts = &cliOptions{}
	}

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Start a chat session (Default)",
		Long:  `The chat command initiates a session with the AI assistant. You can provide a prompt directly as an argument or enter an interactive session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			opts.configPath = configPath
			return c.executeChat(cmd.Context(), opts, args)
		},
	}

	addChatFlags(cmd.Flags(), opts)

	return cmd
}

// executeChat runs the chat command logic.
func (c *chatCommand) executeChat(ctx stdctx.Context, opts *cliOptions, args []string) error {
	// 1. Determine if we are just showing logs
	if opts.showTurnsLog {
		return c.handleStreamLogs(ctx, opts)
	}

	// 2. Load config and apply TUI override
	cfg, err := c.loadConfig(opts, args)
	if err != nil {
		return err
	}

	// 3. Setup Capturer
	capturer, cleanup := c.buildCapturer(ctx, cfg, opts)
	defer func() {
		timeout := ports.DefaultShutdownTimeout
		if !opts.tuiPrompt {
			timeout = 100 * time.Millisecond
		}
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), timeout)
		defer cancel()
		_ = cleanup(shutdownCtx)
	}()

	// 4. Capture Prompt
	prompt, err := c.captureInput(ctx, capturer, opts, args)
	if err != nil {
		return err
	}

	// 5. Delegate business logic to ChatService
	return c.ChatService.ProcessMessage(ctx, cfg, agent.ChatCommand{
		ConfigPath:   opts.configPath,
		NewSession:   opts.newSession,
		LastN:        opts.lastN,
		BackN:        opts.backN,
		RawOutput:    opts.rawOutput,
		UseTUIPrompt: opts.tuiPrompt,
		Retry:        opts.retry,
		Prompt:       prompt,
	}, capturer)
}

func (c *chatCommand) handleStreamLogs(ctx stdctx.Context, opts *cliOptions) error {
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	return c.ChatService.StreamTurnsLog(ctx, cfg, c.Stdout)
}

func (c *chatCommand) loadConfig(opts *cliOptions, args []string) (*domain_config.Config, error) {
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return nil, fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	if opts.tuiPrompt {
		cfg.UseTUIPrompt = true
	}

	// Only auto-enable TUI from config if no other actions are requested
	if cfg != nil && cfg.UseTUIPrompt && len(args) == 0 && opts.lastN == 0 && opts.backN == 0 && !opts.retry {
		opts.tuiPrompt = true
	}

	return cfg, nil
}

func (c *chatCommand) captureInput(ctx stdctx.Context, capturer agent.CapturerInteractor, opts *cliOptions, args []string) (string, error) {
	if opts.retry {
		return "", nil
	}

	captureOpts := c.prepareCaptureOptions(opts)
	prompt, err := capturer.CapturePrompt(ctx, args, captureOpts...)
	if err != nil {
		if !errors.Is(err, ui.ErrNoInput) {
			return "", err
		}
		// Continue with empty prompt if we were told to skip TTY wait (e.g. -l or -b was used)
	}

	return prompt, nil
}

func (c *chatCommand) buildCapturer(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) (agent.CapturerInteractor, func(stdctx.Context) error) {
	if opts.tuiPrompt {
		// Try to get at least the last user message for the trie
		hManager, _ := c.Bootstrapper.GetHistoryManager(ctx, cfg)
		var lastMsg string
		if hManager != nil {
			lastMsg, _, _ = c.ChatService.GetLastUserMessage(ctx, hManager)
		}

		var recentHistory []string
		if lastMsg != "" {
			recentHistory = append(recentHistory, lastMsg)
		}

		svc, err := c.Bootstrapper.GetSuggestionService(ctx, recentHistory)

		capturerInterface := ui.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer, false)
		baseCapturer, ok := capturerInterface.(tui.BaseCapturer)
		if !ok {
			// Fallback: use the base capturer directly if it's an interactor
			if ci, ok := capturerInterface.(agent.CapturerInteractor); ok {
				return ci, func(ctx stdctx.Context) error {
					return ci.Close(ctx)
				}
			}
			return nil, func(stdctx.Context) error { return nil }
		}

		var capturer agent.CapturerInteractor
		var cleanup func(stdctx.Context) error

		if err != nil {
			// Log warning and fall back to the base capturer (no suggestions)
			_, _ = fmt.Fprintf(c.Stderr, "Warning: failed to initialize suggestions: %v\n", err)
			capturer = baseCapturer
			cleanup = func(ctx stdctx.Context) error {
				return capturer.Close(ctx)
			}
		} else {
			capturer = tui.NewPromptCapturer(baseCapturer, svc)
			cleanup = func(ctx stdctx.Context) error {
				if err := capturer.Close(ctx); err != nil {
					_, _ = fmt.Fprintf(c.Stderr, "Warning: failed to close capturer: %v\n", err)
					return err
				}
				return nil
			}
		}

		if sm, ok := c.SM.(interface {
			SetInteractor(domain_security.UserInteractor)
		}); ok {
			// capturer already implements UserInteractor via CapturerInteractor
			sm.SetInteractor(capturer)
		}
		return capturer, cleanup
	}
	return c.setupCapturer()
}

func (c *chatCommand) setupCapturer() (agent.CapturerInteractor, func(stdctx.Context) error) {
	capturerInterface := ui.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer, false)
	capturer, ok := capturerInterface.(agent.CapturerInteractor)
	if !ok {
		return nil, func(stdctx.Context) error { return nil }
	}
	if sm, ok := c.SM.(interface {
		SetInteractor(domain_security.UserInteractor)
	}); ok {
		sm.SetInteractor(capturerInterface)
	}
	return capturer, func(ctx stdctx.Context) error {
		return capturer.Close(ctx)
	}
}

func (c *chatCommand) prepareCaptureOptions(opts *cliOptions) []ports.CaptureOption {
	var captureOpts []ports.CaptureOption
	if opts.lastN > 0 || opts.backN > 0 {
		captureOpts = append(captureOpts, ports.WithSkipTTYWait(true))
	}
	if opts.rawOutput {
		captureOpts = append(captureOpts, ports.WithRaw(true))
	}
	if opts.tuiPrompt {
		captureOpts = append(captureOpts, ports.WithTUIPrompt(true))
	}
	return captureOpts
}
