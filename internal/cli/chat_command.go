// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

func init() {
	register("chat", func(ctx *context) command {
		return newChatCommand(ctx)
	})
}

// chatCommand implements the main chat command.
type chatCommand struct {
	Version     string
	HomeDir     string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	SM          domain_security.Manager
	ChatService agent.ChatService
	MockPrompt  string
	MockAnswer  string
}

type cliOptions struct {
	configPath  string
	newSession  bool
	showVersion bool
	lastN       int
	backN       int
	rawOutput   bool
	retry       bool
}

// newChatCommand creates a new Chat Command with default factories.
func newChatCommand(ctx *context) *chatCommand {
	return &chatCommand{
		Version:     ctx.Version,
		HomeDir:     ctx.HomeDir,
		Stdin:       ctx.Stdin,
		Stdout:      ctx.Stdout,
		Stderr:      ctx.Stderr,
		SM:          ctx.SM,
		ChatService: ctx.ChatService,
		MockPrompt:  ctx.MockPrompt,
		MockAnswer:  ctx.MockAnswer,
	}
}

// Execute runs the chat command logic.
func (c *chatCommand) Execute(ctx stdctx.Context, args []string) error {
	// 1. Parsing command-line flags and arguments
	opts, fs, err := c.parseConfiguration(args)
	if err != nil {
		return err
	}
	if opts.showVersion {
		_, _ = fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
		return nil
	}

	// 2. Invoking a Use Case / Service interface
	capturer := ui.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM, clock.RealClock{}, c.MockPrompt, c.MockAnswer).(orchestration.Capturer)
	if sm, ok := c.SM.(interface {
		SetInteractor(domain_security.UserInteractor)
	}); ok {
		sm.SetInteractor(capturer.(domain_security.UserInteractor))
	}

	var captureOpts []orchestration.CaptureOption
	if opts.lastN > 0 || opts.backN > 0 || opts.retry {
		captureOpts = append(captureOpts, orchestration.WithSkipTTYWait(true))
	}
	if opts.rawOutput {
		captureOpts = append(captureOpts, orchestration.WithRaw(true))
	}

	prompt, err := capturer.CapturePrompt(ctx, fs, captureOpts...)
	if err != nil {
		if !errors.Is(err, ui.ErrNoInput) {
			return err
		}
		// Continue with empty prompt if we were told to skip TTY wait (e.g. -l or -b was used)
		prompt = ""
	}

	if opts.retry {
		lastMsg, turns, err := c.ChatService.GetLastUserMessage(ctx, opts.configPath, capturer)
		if err != nil {
			return fmt.Errorf("failed to get last user message for retry: %w", err)
		}
		if lastMsg == "" {
			return errors.New("no previous user message found to retry")
		}

		interactor, ok := capturer.(domain_security.UserInteractor)
		if !ok {
			return errors.New("the provided terminal capturer does not support user interaction prompts")
		}

		confirmed, err := interactor.Confirm(ctx, fmt.Sprintf("Retry: %q?", lastMsg))
		if err != nil {
			return fmt.Errorf("failed to prompt for retry confirmation: %w", err)
		}
		if !confirmed {
			return nil
		}
		prompt = lastMsg
		opts.backN = turns
	}

	// Delegate all business logic and orchestration to the ChatService
	return c.ChatService.ProcessMessage(ctx, agent.ChatOptions{
		ConfigPath: opts.configPath,
		NewSession: opts.newSession,
		LastN:      opts.lastN,
		BackN:      opts.backN,
		RawOutput:  opts.rawOutput,
		Prompt:     prompt,
	}, capturer)
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
	fs.IntVar(&opts.lastN, "l", 0, "Show the last N messages from history")
	fs.IntVar(&opts.backN, "b", 0, "Go back / delete the last N turns from history")
	fs.BoolVar(&opts.rawOutput, "r", false, "Show raw output (without markdown rendering)")
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
