// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"strings"

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

// errExitZero signals that the command should exit with code 0 immediately.
var errExitZero = errors.New("exit zero")

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

// newChatCommand creates a new Chat Command as a Cobra command.
func newChatCommand(ctx *context) *cobra.Command {
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

	opts := &cliOptions{}

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Start a chat session (Default)",
		Long:  `The chat command initiates a session with the AI assistant. You can provide a prompt directly as an argument or enter an interactive session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			opts.configPath = configPath
			return c.execute(cmd.Context(), cmd.Flags(), opts, args)
		},
	}

	c.addFlags(cmd.Flags(), opts)

	return cmd
}

func (c *chatCommand) addFlags(fs *pflag.FlagSet, opts *cliOptions) {
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVarP(&opts.showTurnsLog, "turns", "t", false, "Print the contents of the current session's turns.log and exit")
	fs.IntVarP(&opts.lastN, "last", "l", 0, "Show the last N messages from history")
	fs.Lookup("last").NoOptDefVal = "1"
	fs.IntVarP(&opts.backN, "back", "b", 0, "Go back / delete the last N turns from history")
	fs.Lookup("back").NoOptDefVal = "1"
	fs.BoolVarP(&opts.rawOutput, "raw", "r", false, "Show raw output (without markdown rendering)")
	fs.BoolVarP(&opts.tuiPrompt, "interactive", "i", false, "Enable interactive TUI prompt with suggestions")
	fs.BoolVar(&opts.tuiPrompt, "tui", false, "Enable interactive TUI prompt with suggestions")
	fs.BoolVar(&opts.retry, "retry", false, "Retry the last user message")
}

// execute runs the chat command logic.
func (c *chatCommand) execute(ctx stdctx.Context, fs *pflag.FlagSet, opts *cliOptions, args []string) error {
	// 1. Determine if we are just showing logs
	if opts.showTurnsLog {
		cfg, err := c.Loader.Load(opts.configPath)
		if err != nil {
			return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
		}

		return c.ChatService.StreamTurnsLog(ctx, cfg, c.Stdout)
	}

	// 2. Handle Retry Flow
	var prompt string
	var err error
	if opts.retry {
		var abort bool
		prompt, opts.backN, abort, err = c.handleRetryFlow(ctx, opts)
		if err != nil {
			return err
		}
		if abort {
			return nil
		}
	}

	// 3. Load config and apply TUI override
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	if opts.tuiPrompt {
		cfg.UseTUIPrompt = true
	}

	// Only auto-enable TUI from config if no other actions are requested
	if cfg != nil && cfg.UseTUIPrompt && len(args) == 0 && opts.lastN == 0 && opts.backN == 0 && !opts.retry {
		opts.tuiPrompt = true
	}

	// 4. Setup Capturer
	capturer, cleanup := c.buildCapturer(ctx, cfg, opts)
	defer func() {
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		_ = cleanup(shutdownCtx)
	}()

	// 5. Capture Prompt (if not retry)
	if !opts.retry {
		captureOpts := c.prepareCaptureOptions(opts)
		prompt, err = capturer.CapturePrompt(ctx, args, captureOpts...)
		if err != nil {
			if !errors.Is(err, ui.ErrNoInput) {
				return err
			}
			// Continue with empty prompt if we were told to skip TTY wait (e.g. -l or -b was used)
		}
	}

	// 6. Delegate business logic to ChatService
	return c.ChatService.ProcessMessage(ctx, cfg, agent.ChatOptions{
		ConfigPath:   opts.configPath,
		NewSession:   opts.newSession,
		LastN:        opts.lastN,
		BackN:        opts.backN,
		RawOutput:    opts.rawOutput,
		UseTUIPrompt: opts.tuiPrompt,
		Prompt:       prompt,
	}, capturer)
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
			// Fallback: use a dummy cleanup or return an error if TUI requires BaseCapturer
			ci, ok := capturerInterface.(agent.CapturerInteractor)
			if !ok {
				return nil, func(stdctx.Context) error { return nil }
			}
			return ci, func(stdctx.Context) error { return nil }
		}

		var capturer agent.CapturerInteractor
		cleanup := func(stdctx.Context) error { return nil }

		if err != nil {
			// Log warning and fall back to the base capturer (no suggestions)
			_, _ = fmt.Fprintf(c.Stderr, "Warning: failed to initialize suggestions: %v\n", err)
			capturer = baseCapturer
		} else {
			capturer = tui.NewPromptCapturer(baseCapturer, svc)
			cleanup = func(ctx stdctx.Context) error {
				if err := capturer.Close(ctx); err != nil {
					_, _ = fmt.Fprintf(c.Stderr, "Warning: failed to close suggestion service: %v\n", err)
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
	return capturer, func(stdctx.Context) error { return nil }
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

func (c *chatCommand) handleRetryFlow(ctx stdctx.Context, opts *cliOptions) (prompt string, backN int, abort bool, err error) {
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return "", 0, false, fmt.Errorf("failed to load config for retry: %w", err)
	}

	hManager, err := c.Bootstrapper.GetHistoryManager(ctx, cfg)
	if err != nil {
		return "", 0, false, fmt.Errorf("failed to get history manager for retry: %w", err)
	}

	lastMsg, turns, err := c.ChatService.GetLastUserMessage(ctx, hManager)
	if err != nil {
		return "", 0, false, fmt.Errorf("failed to get last user message for retry: %w", err)
	}
	if lastMsg == "" {
		return "", 0, false, errors.New("no previous user message found to retry")
	}

	_, _ = fmt.Fprintf(c.Stdout, "Are you sure you want to retry the following message?\n\n%s\n\nRetry? [y/N]: ", lastMsg)
	var response string
	_, _ = fmt.Fscanln(c.Stdin, &response)
	if strings.ToLower(strings.TrimSpace(response)) != "y" {
		return "", 0, true, nil // User aborted
	}
	return lastMsg, turns, false, nil
}
