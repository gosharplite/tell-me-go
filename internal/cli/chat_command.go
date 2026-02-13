// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

func init() {
	register("chat", func(ctx *context) command {
		return newChatCommand(ctx)
	})
}

// chatCommand implements the main chat command.
type chatCommand struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	HomeDir string
	SM      domain_security.ISecurityManager

	AgentFactory  func(client *llm.Client, hManager *history.Manager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing domain_pricing.PricingData, bus events.EventBus) (*llm.Client, error)
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
	return &chatCommand{
		Version: ctx.Version,
		Stdin:   ctx.Stdin,
		Stdout:  ctx.Stdout,
		Stderr:  ctx.Stderr,
		HomeDir: ctx.HomeDir,
		SM:      ctx.SM,
		AgentFactory: func(client *llm.Client, hManager *history.Manager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
			return agent.New(client, hManager, reg, sm, disableStreaming, bus,
				agent.WithPricing(model, mode, pricingOverrides),
				agent.WithSessionCostTracker(tracker),
				agent.WithInternalTools(),
				agent.WithTraceLogger(logPath),
			)
		},
		ClientFactory: func(cfg *config.Config, pricing domain_pricing.PricingData, bus events.EventBus) (*llm.Client, error) {
			authenticator := &auth.VertexAuth{}
			maxBudget := cfg.ResolveThinkingBudget(cfg.Model, pricing)
			return llm.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch, bus)
		},
	}
}

// Execute runs the chat command logic.
func (c *chatCommand) Execute(ctx stdctx.Context, args []string) error {
	capturer := ui.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM)
	if sm, ok := c.SM.(interface {
		SetInteractor(domain_security.UserInteractor)
	}); ok {
		sm.SetInteractor(capturer)
	}

	opts, fs, cfg, err := c.initializeConfiguration(args)
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

	orch := orchestration.NewOrchestrator(c.HomeDir, c.Version, c.SM, c.Stdout, c.Stderr)
	if c.AgentFactory != nil {
		orch.AgentFactory = c.AgentFactory
	}
	if c.ClientFactory != nil {
		orch.ClientFactory = c.ClientFactory
	}

	sCfg := &orchestration.SessionConfig{
		ConfigPath: opts.configPath,
		NewSession: opts.newSession,
		LastN:      opts.lastN,
		RawOutput:  opts.rawOutput,
		Prompt:     prompt,
		Config:     cfg,
	}

	return orch.Run(ctx, sCfg, capturer)
}

func (c *chatCommand) initializeConfiguration(args []string) (*cliOptions, *flag.FlagSet, *config.Config, error) {
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
	fs.StringVar(&opts.configPath, "c", "configs/vertex.yaml", "Path to the configuration file")
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
