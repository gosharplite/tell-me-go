// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/gosharplite/tell-me-go/internal/ui/tui"
)

// errExitZero signals that the command should exit with code 0 immediately.
var errExitZero = errors.New("exit zero")

func init() {
	register("chat", func(ctx *context) command {
		return newChatCommand(ctx)
	})
}

// chatCommand implements the main chat command.
type chatCommand struct {
	Version      string
	HomeDir      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	SM           domain_security.Manager
	ChatService  agent.ChatService
	Bootstrapper Bootstrapper
	Loader       domain_config.ConfigLoader
	FileSystem   infra_persistence.FileSystem
	MockPrompt   string
	MockAnswer   string
}

type cliOptions struct {
	configPath   string
	newSession   bool
	showVersion  bool
	showTurnsLog bool
	lastN        int
	backN        int
	rawOutput    bool
	tuiPrompt    bool
	retry        bool
}

// newChatCommand creates a new Chat Command with default factories.
func newChatCommand(ctx *context) *chatCommand {
	return &chatCommand{
		Version:      ctx.Version,
		HomeDir:      ctx.HomeDir,
		Stdin:        ctx.Stdin,
		Stdout:       ctx.Stdout,
		Stderr:       ctx.Stderr,
		SM:           ctx.SM,
		ChatService:  ctx.ChatService,
		Bootstrapper: ctx.Bootstrapper,
		Loader:       ctx.Loader,
		FileSystem:   ctx.FileSystem,
		MockPrompt:   ctx.MockPrompt,
		MockAnswer:   ctx.MockAnswer,
	}
}

// Execute runs the chat command logic.
func (c *chatCommand) Execute(ctx stdctx.Context, args []string) error {
	opts, fs, err := c.resolveOptions(args)
	if err != nil {
		if errors.Is(err, errExitZero) {
			return nil
		}
		return err
	}

	if opts.showTurnsLog {
		cfg, err := c.Loader.Load(opts.configPath)
		if err != nil {
			return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
		}

		// Resolve home directory (adjust if your CLI context provides this natively)
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to determine home directory: %w", err)
		}

		// Initialize lightweight paths directly without booting the agent
		paths, err := infra_persistence.InitializePaths(c.FileSystem, homeDir, cfg.Mode)
		if err != nil || paths.TurnsLogPath == "" {
			return errors.New("turns log path not available")
		}

		// Use the injected FileSystem abstraction, NOT os.Open
		file, err := c.FileSystem.Open(paths.TurnsLogPath)
		if err != nil {
			return fmt.Errorf("failed to open turns log at %s: %w", paths.TurnsLogPath, err)
		}
		defer file.Close()

		// Stream the file to Stdout to avoid high memory allocation on large logs
		if _, err := io.Copy(c.Stdout, file); err != nil {
			return fmt.Errorf("failed to output turns log: %w", err)
		}
		return nil
	}

	var prompt string
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

	// 3. Invoking a Use Case / Service interface
	cfg, err := c.Loader.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	capturer, cleanup := c.buildCapturer(ctx, cfg, opts)
	defer func() {
		shutdownCtx, cancel := stdctx.WithTimeout(stdctx.Background(), ports.DefaultShutdownTimeout)
		defer cancel()
		_ = cleanup(shutdownCtx)
	}()

	if !opts.retry {
		prompt, err = c.capturePrompt(ctx, fs, opts, capturer)
		if err != nil {
			return err
		}
	}

	// Delegate all business logic and orchestration to the ChatService
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

func (c *chatCommand) resolveOptions(args []string) (*cliOptions, *flag.FlagSet, error) {
	opts, fs, err := c.parseConfiguration(args)
	if err != nil {
		return nil, nil, err
	}
	if opts.showVersion {
		_, _ = fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
		return nil, nil, errExitZero
	}

	// Configuration Merge
	cfg, _ := c.Loader.Load(opts.configPath)
	// Only auto-enable TUI from config if no other actions are requested
	if cfg != nil && cfg.UseTUIPrompt && fs.NArg() == 0 && opts.lastN == 0 && opts.backN == 0 && !opts.retry {
		opts.tuiPrompt = true
	}

	return opts, fs, nil
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

func (c *chatCommand) capturePrompt(ctx stdctx.Context, fs *flag.FlagSet, opts *cliOptions, capturer agent.CapturerInteractor) (string, error) {
	captureOpts := c.prepareCaptureOptions(opts)
	prompt, err := capturer.CapturePrompt(ctx, fs, captureOpts...)
	if err != nil {
		if !errors.Is(err, ui.ErrNoInput) {
			return "", err
		}
		// Continue with empty prompt if we were told to skip TTY wait (e.g. -l or -b was used)
		return "", nil
	}
	return prompt, nil
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

func (c *chatCommand) parseConfiguration(args []string) (*cliOptions, *flag.FlagSet, error) {
	args = c.sanitizeArgs(args)
	var flagArgs []string
	if len(args) > 0 {
		flagArgs = args[1:]
	}

	fs := flag.NewFlagSet("tell-me-go", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	opts := &cliOptions{}
	fs.StringVar(&opts.configPath, "c", "configs/assistant.yaml", "Path to the configuration file")
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVar(&opts.showVersion, "v", false, "Show version information")
	fs.BoolVar(&opts.showTurnsLog, "t", false, "Print the contents of the current session's turns.log and exit")
	fs.IntVar(&opts.lastN, "l", 0, "Show the last N messages from history")
	fs.IntVar(&opts.backN, "b", 0, "Go back / delete the last N turns from history")
	fs.BoolVar(&opts.rawOutput, "r", false, "Show raw output (without markdown rendering)")
	fs.BoolVar(&opts.tuiPrompt, "i", false, "Enable interactive TUI prompt with suggestions")
	fs.BoolVar(&opts.tuiPrompt, "tui", false, "Enable interactive TUI prompt with suggestions")
	fs.BoolVar(&opts.retry, "retry", false, "Retry the last user message")

	if err := fs.Parse(flagArgs); err != nil {
		return nil, nil, err
	}
	return opts, fs, nil
}

func (c *chatCommand) sanitizeArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}
	processed := args[1:]
	for i, arg := range processed {
		if arg == "-l" || arg == "-b" {
			isNextNum := false
			if i+1 < len(processed) {
				if _, err := strconv.Atoi(processed[i+1]); err == nil {
					isNextNum = true
				}
			}
			if !isNextNum {
				newArgs := make([]string, 0, len(args)+1)
				newArgs = append(newArgs, args[:i+2]...)
				newArgs = append(newArgs, "1")
				newArgs = append(newArgs, args[i+2:]...)
				return newArgs
			}
		}
	}
	return args
}
