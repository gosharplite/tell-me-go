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
	Version          string
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	SM               domain_security.Manager
	ChatService      agent.ChatService
	Bootstrapper     Bootstrapper
	Loader           domain_config.ConfigLoader
	HomeDir          string
	MockPrompt       string
	MockAnswer       string
	Interactor       *InteractorRef
	capturerOverride agent.CapturerInteractor                                                                                                                                                                // test-only injection
	capturerFactory  func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor // test-only injection; defaults to ui.NewCapturer
}

// newCapturer calls the injected factory or falls back to ui.NewCapturer.
func (c *chatCommand) newCapturer(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
	if c.capturerFactory != nil {
		return c.capturerFactory(stdin, stdout, stderr, sm, clk, mockPrompt, mockAnswer, disableEscapeSequences)
	}
	return ui.NewCapturer(stdin, stdout, stderr, sm, clk, mockPrompt, mockAnswer, disableEscapeSequences)
}

// warnf writes a formatted warning message to stderr; errors from Fprintf are
// deliberately discarded because there is no recovery path for logging failures.
func (c *chatCommand) warnf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(c.Stderr, format, args...)
}

type cliOptions struct {
	configPath   string
	newSession   bool
	showTurnsLog bool
	diagnostic   bool
	jsonOutput   bool
	lastN        int
	backN        int
	rawOutput    bool
	tuiPrompt    bool
	retry        bool
}

func addChatFlags(fs *pflag.FlagSet, opts *cliOptions) {
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVarP(&opts.showTurnsLog, "turns", "t", false, "Print the contents of the current session's turns.log and exit")
	fs.BoolVarP(&opts.diagnostic, "diagnostic", "d", false, "Run a comprehensive system health check and exit")
	fs.BoolVar(&opts.jsonOutput, "json", false, "Output in JSON format (for diagnostics)")
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
		Interactor:   ctx.Interactor,
	}
	c.capturerFactory = ui.NewCapturer

	if opts == nil {
		opts = &cliOptions{}
	}

	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Start a chat session (Default)",
		Long:  `The chat command initiates a session with the AI assistant. You can provide a prompt directly as an argument or enter an interactive session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config") // flag guaranteed by root command; never errors
			opts.configPath = configPath
			return c.executeChat(cmd.Context(), opts, args)
		},
	}

	addChatFlags(cmd.Flags(), opts)

	return cmd
}

// executeChat runs the chat command logic.
func (c *chatCommand) executeChat(ctx stdctx.Context, opts *cliOptions, args []string) error {
	// 1. Load config once for all subsequent paths
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	// 2. Handle Diagnostic mode
	if opts.diagnostic {
		return c.handleDiagnosticWorkflow(ctx, cfg, opts)
	}

	// 3. Handle Turns Log streaming
	if opts.showTurnsLog {
		return c.handleTurnsLogWorkflow(ctx, cfg, opts)
	}

	// 4. Setup chat session (TUI logic + capturer setup)
	capturer, cleanup, err := c.setupChatSession(ctx, cfg, opts, args)
	if err != nil {
		return err
	}
	defer func() {
		timeout := ports.DefaultShutdownTimeout
		if !opts.tuiPrompt {
			timeout = 100 * time.Millisecond
		}
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), timeout)
		defer cancel()
		_ = cleanup(shutdownCtx)
	}()

	// 5. Process chat request (prompt capture + delegation)
	return c.processChatRequest(ctx, cfg, opts, args, capturer)
}

func (c *chatCommand) handleDiagnosticWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) error {
	return c.ChatService.RunDiagnostics(ctx, cfg, opts.configPath, opts.jsonOutput)
}

func (c *chatCommand) handleTurnsLogWorkflow(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) error {
	return c.ChatService.StreamTurnsLog(ctx, cfg, c.Stdout)
}

func (c *chatCommand) isTUIConfigured(cfg *domain_config.Config) bool {
	return cfg != nil && cfg.UseTUIPrompt
}

func (c *chatCommand) noOtherActionsRequested(opts *cliOptions, args []string) bool {
	return len(args) == 0 && opts.lastN == 0 && opts.backN == 0 && !opts.retry
}

func (c *chatCommand) setupChatSession(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string) (agent.CapturerInteractor, func(stdctx.Context) error, error) {
	// Apply TUI overrides and state detection
	if opts.tuiPrompt {
		cfg.UseTUIPrompt = true
	}
	// Only auto-enable TUI from config if no other actions are requested
	if c.isTUIConfigured(cfg) && c.noOtherActionsRequested(opts, args) {
		opts.tuiPrompt = true
	}
	return c.getCapturer(ctx, cfg, opts)
}

// getCapturer returns the override if set (test path), otherwise delegates
// to buildCapturer for the production path.
func (c *chatCommand) getCapturer(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) (agent.CapturerInteractor, func(stdctx.Context) error, error) {
	if c.capturerOverride != nil {
		return c.capturerOverride, func(shutdownCtx stdctx.Context) error {
			if err := c.capturerOverride.Close(shutdownCtx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}, nil
	}
	return c.buildCapturer(ctx, cfg, opts)
}
func (c *chatCommand) processChatRequest(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions, args []string, capturer agent.CapturerInteractor) error {
	prompt, err := c.captureInput(ctx, capturer, opts, args)
	if err != nil {
		return err
	}
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

// buildTUICapturer constructs a TUI-based prompt capturer with suggestion support.
// It seeds the suggestion trie from the last user message (best-effort), initializes
// the suggestion service, and wraps the base capturer. Falls back to a bare capturer
// if suggestion initialization fails.
func (c *chatCommand) buildTUICapturer(ctx stdctx.Context, cfg *domain_config.Config) (agent.CapturerInteractor, func(stdctx.Context) error, error) {
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

	capturerInterface := c.newCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer, false)
	baseCapturer, ok := capturerInterface.(tui.BaseCapturer)
	if !ok {
		// Coverage gap accepted by architect: structurally unreachable.
		// tui.BaseCapturer and agent.CapturerInteractor are structurally
		// identical interfaces (both compose ports.Capturer +
		// domain_security.UserInteractor). Any type that fails the
		// BaseCapturer assertion necessarily also fails the
		// CapturerInteractor assertion — and vice versa. The dual-failure
		// path is covered by the error return below. See Issue #888.
		return nil, nil, fmt.Errorf("ui.NewCapturer did not return a tui.BaseCapturer or agent.CapturerInteractor")
	}

	var capturer agent.CapturerInteractor
	var cleanup func(stdctx.Context) error

	if err != nil {
		// Log warning and fall back to the base capturer (no suggestions)
		c.warnf("Warning: failed to initialize suggestions: %v\n", err)
		capturer = baseCapturer
		cleanup = func(ctx stdctx.Context) error {
			return capturer.Close(ctx)
		}
	} else {
		capturer = tui.NewPromptCapturer(baseCapturer, svc)
		cleanup = func(ctx stdctx.Context) error {
			if err := capturer.Close(ctx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}
	}

	c.Interactor.set(capturer)
	return capturer, cleanup, nil
}

func (c *chatCommand) buildCapturer(ctx stdctx.Context, cfg *domain_config.Config, opts *cliOptions) (agent.CapturerInteractor, func(stdctx.Context) error, error) {
	if opts.tuiPrompt {
		return c.buildTUICapturer(ctx, cfg)
	}
	capturer, cleanup, err := c.setupCapturer()
	if err != nil {
		return nil, nil, err
	}
	return capturer, cleanup, nil
}

func (c *chatCommand) setupCapturer() (agent.CapturerInteractor, func(stdctx.Context) error, error) {
	if c.capturerOverride != nil {
		return c.capturerOverride, func(ctx stdctx.Context) error {
			if err := c.capturerOverride.Close(ctx); err != nil {
				c.warnf("Warning: failed to close capturer: %v\n", err)
				return err
			}
			return nil
		}, nil
	}

	capturerInterface := c.newCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer, false)
	capturer, ok := capturerInterface.(agent.CapturerInteractor)
	if !ok {
		return nil, nil, fmt.Errorf("ui.NewCapturer did not return an agent.CapturerInteractor")
	}
	c.Interactor.set(capturer)
	return capturer, func(ctx stdctx.Context) error {
		if err := capturer.Close(ctx); err != nil {
			c.warnf("Warning: failed to close capturer: %v\n", err)
			return err
		}
		return nil
	}, nil
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
