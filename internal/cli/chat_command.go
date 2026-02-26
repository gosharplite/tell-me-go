// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/di"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

func init() {
	register("chat", func(ctx *context) command {
		return newChatCommand(ctx)
	})
}

// chatCommand implements the main chat command.
type chatCommand struct {
	Version   string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	HomeDir   string
	SM        domain_security.ISecurityManager
	Loader    domain_config.ConfigLoader
	Container di.Container
}

type cliOptions struct {
	configPath  string
	newSession  bool
	showVersion bool
	lastN       int
	rawOutput   bool
}

// newChatCommand creates a new Chat Command with default factories.
func newChatCommand(ctx *context) *chatCommand {
	container := di.NewBootstrapper(ctx.HomeDir, ctx.SM, ctx.Version, ctx.Stdout, ctx.Stderr, nil)
	return &chatCommand{
		Version:   ctx.Version,
		Stdin:     ctx.Stdin,
		Stdout:    ctx.Stdout,
		Stderr:    ctx.Stderr,
		HomeDir:   ctx.HomeDir,
		SM:        ctx.SM,
		Loader:    &config.YAMLConfigLoader{},
		Container: container,
	}
}

// Execute runs the chat command logic.
func (c *chatCommand) Execute(ctx stdctx.Context, args []string) error {
	capturer, opts, fs, cfg, err := c.initializeCLI(args)
	if err != nil {
		return err
	}
	if opts.showVersion {
		fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
		return nil
	}

	prompt, err := capturer.CapturePrompt(ctx, fs, opts.lastN, opts.rawOutput)
	if err != nil {
		return err
	}

	deps, hManager, cleanup, err := c.Container.BuildSessionDependencies(ctx, cfg, opts.configPath, opts.newSession, capturer.(domain_security.UserInteractor))
	if err != nil {
		return err
	}

	defer cleanup()

	defer func() {
		if shutdownErr := deps.GetEventBus().Shutdown(ctx); shutdownErr != nil {
			fmt.Fprintf(c.Stderr, "Warning: Event bus shutdown failed: %v\n", shutdownErr)
		}
	}()

	err = c.performChat(ctx, capturer, opts, prompt, cfg, deps)

	c.Container.FinalizeSession(ctx, hManager, deps, cfg)

	return err
}

func (c *chatCommand) initializeCLI(args []string) (orchestration.Capturer, *cliOptions, *flag.FlagSet, *domain_config.Config, error) {
	capturer := ui.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM)
	if sm, ok := c.SM.(interface {
		SetInteractor(domain_security.UserInteractor)
	}); ok {
		sm.SetInteractor(capturer)
	}

	opts, fs, cfg, err := c.initializeConfiguration(args)
	return capturer.(orchestration.Capturer), opts, fs, cfg, err
}

func (c *chatCommand) performChat(ctx stdctx.Context, capturer orchestration.Capturer, opts *cliOptions, prompt string, cfg *domain_config.Config, deps ports.SessionDependencies) error {
	uiRenderer := ui.NewRenderer(c.SM, c.Stdout, c.Stderr)
	historyRenderer := &ui.StdHistoryRenderer{}
	orch := orchestration.NewOrchestrator(c.HomeDir, c.Version, c.Loader, c.SM, c.Stdout, c.Stderr, c.Container.GetAgentFactory(), historyRenderer, uiRenderer)

	sCfg := orchestration.NewSessionConfig(opts.configPath, opts.newSession, opts.lastN, opts.rawOutput, prompt, cfg)
	return orch.Run(ctx, sCfg, deps, capturer)
}

func (c *chatCommand) initializeConfiguration(args []string) (*cliOptions, *flag.FlagSet, *domain_config.Config, error) {
	args = c.sanitizeArgs(args)
	var flagArgs []string
	if len(args) > 0 {
		flagArgs = args[1:]
	}
	opts, fs, err := c.parseFlags(flagArgs)
	if err != nil {
		return nil, nil, nil, err
	}
	if opts.showVersion {
		return opts, fs, nil, nil
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}
	return opts, fs, cfg, nil
}

func (c *chatCommand) parseFlags(args []string) (*cliOptions, *flag.FlagSet, error) {
	fs := flag.NewFlagSet("tell-me-go", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)
	opts := &cliOptions{}
	fs.StringVar(&opts.configPath, "c", "configs/assistant.yaml", "Path to the configuration file")
	fs.BoolVar(&opts.newSession, "new", false, "Start a new session")
	fs.BoolVar(&opts.showVersion, "v", false, "Show version information")
	fs.IntVar(&opts.lastN, "l", 0, "Show the last N messages from history")
	fs.BoolVar(&opts.rawOutput, "r", false, "Show raw output (without markdown rendering)")
	if err := fs.Parse(args); err != nil {
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
		if arg == "-l" {
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
