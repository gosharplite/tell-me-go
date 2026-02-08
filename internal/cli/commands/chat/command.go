// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/ui"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/cli/command"
	"github.com/gosharplite/tell-me-go/internal/config"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/session"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/ui/input"
	"github.com/gosharplite/tell-me-go/internal/ui/render"
)

func init() {
	command.Register("chat", func(ctx *command.Context) command.Command {
		return NewCommand(ctx)
	})
}

// Command implements the main chat command.
type Command struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	HomeDir string
	SM      *internal_security.SecurityManager

	AgentFactory  func(client *llm.Client, hManager *history.Manager, registry domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing pricing.PricingData) (*llm.Client, error)
}

type cliOptions struct {
	configPath  string
	newSession  bool
	showVersion bool
	lastN       int
	rawOutput   bool
}

// NewCommand creates a new Chat Command with default factories.
func NewCommand(ctx *command.Context) *Command {
	return &Command{
		Version: ctx.Version,
		Stdin:   ctx.Stdin,
		Stdout:  ctx.Stdout,
		Stderr:  ctx.Stderr,
		HomeDir: ctx.HomeDir,
		SM:      ctx.SM,
		AgentFactory: func(client *llm.Client, hManager *history.Manager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
			return agent.New(client, hManager, reg, sm, disableStreaming,
				agent.WithPricing(model, mode, pricingOverrides),
				agent.WithSessionCostTracker(tracker),
			)
		},
		ClientFactory: func(cfg *config.Config, pricing pricing.PricingData) (*llm.Client, error) {
			authenticator := &auth.VertexAuth{}
			maxBudget := cfg.ResolveThinkingBudget(cfg.Model, pricing)
			return llm.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch)
		},
	}
}

// Execute runs the chat command logic.
func (c *Command) Execute(ctx context.Context, args []string) error {
	c.SM.SetInputReader(c.Stdin)
	capturer := input.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM)

	opts, fs, cfg, err := c.initializeConfiguration(args)
	if err != nil {
		return err
	}
	if opts.showVersion {
		fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
		return nil
	}

	prompt, err := capturer.Prompt(ctx, fs, opts.lastN, opts.rawOutput)
	if err != nil {
		return err
	}

	paths, err := session.InitializePaths(c.HomeDir, cfg.Mode)
	if err != nil {
		return err
	}

	pricingOverrides := c.getPricingOverrides(cfg)
	c.setupSecurity(paths, opts.configPath)
	if opts.newSession {
		c.handleNewSession(ctx, paths, cfg, pricingOverrides)
	}

	hManager, client, registry, tracker, pruned, pData, err := c.initializeDependencies(ctx, *paths, cfg, pricingOverrides)
	if err != nil {
		return err
	}

	if opts.lastN > 0 {
		render.History(c.Stdout, hManager, opts.lastN, render.RenderOptions{
			Raw:          opts.rawOutput,
			ShowThoughts: cfg.ShowThoughts,
			UseColor:     capturer.IsTTY(c.Stdout) && !opts.rawOutput,
		})
	}
	if prompt == "" && opts.lastN > 0 {
		return nil
	}

	chatAgent := c.AgentFactory(client, hManager, registry, c.SM, cfg.DisableStreaming, cfg.Model, cfg.Mode, pricingOverrides, tracker)
	c.applyConfiguration(chatAgent, cfg, opts, paths, pruned, pData, capturer)

	sess := agent.NewSession(hManager)
	sess.PrunedTurns = pruned
	if err := chatAgent.Chat(ctx, sess, prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return c.finalizeSession(ctx, chatAgent, hManager, *paths, cfg, pricingOverrides)
}

func (c *Command) initializeConfiguration(args []string) (*cliOptions, *flag.FlagSet, *config.Config, error) {
	args = c.sanitizeArgs(args)
	opts, fs, err := c.parseFlags(args[1:])
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

func (c *Command) initializeDependencies(ctx context.Context, paths session.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) (*history.Manager, *llm.Client, domaintools.IToolRegistry, domain_pricing.ICostTracker, int, pricing.PricingData, error) {
	hManager := history.NewManager(paths.HistoryPath)
	if err := hManager.Load(ctx); err != nil {
		return nil, nil, nil, nil, 0, pricing.PricingData{}, fmt.Errorf("error loading history: %w", err)
	}
	pruned, _ := hManager.Prune(ctx, cfg.MaxHistoryTurns)
	pricingData := framework.GetPricing(ctx, c.SM, filepath.Join(c.HomeDir, "output"))
	hManager.Snapshot()

	client, err := c.ClientFactory(cfg, pricingData)
	if err != nil {
		return nil, nil, nil, nil, 0, pricingData, fmt.Errorf("error creating client: %w", err)
	}

	registry := c.setupRegistry(client, cfg, &paths, pricingOverrides)
	modelPricing := framework.GetModelPricing(cfg.Model, pricingData)
	tracker := framework.NewSessionCostTracker(c.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	return hManager, client, registry, tracker, pruned, pricingData, nil
}

func (c *Command) finalizeSession(ctx context.Context, chatAgent agent.Chatter, hManager *history.Manager, paths session.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) error {
	if err := hManager.Save(ctx); err != nil {
		return fmt.Errorf("error saving history: %w", err)
	}
	if err := framework.RecordSessionCost(ctx, c.SM, chatAgent.GetCostTracker(), paths.LogPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}
	return nil
}

func (c *Command) getPricingOverrides(cfg *config.Config) map[string]pricing.ModelPricing {
	pricingOverrides := make(map[string]pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (c *Command) setupSecurity(paths *session.Paths, configPath string) {
	c.SM.SetSafePathsFile(paths.SafePathsPath)
	c.SM.SetReadOnlyPathsFile(paths.ReadPathsPath)
	c.SM.SetBypassFile(paths.BypassPath)
	c.SM.SetCommandsLogFile(paths.CommandsLogPath)
	if err := c.SM.LoadSafePaths(); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to load safe paths: %v\n", err)
	}
	if err := c.SM.LoadReadOnlyPaths(); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to load read-only paths: %v\n", err)
	}
	c.SM.LoadBypassState()
	c.SM.RegisterSafePath(filepath.Join(c.HomeDir, "output"))
	c.SM.RegisterReadOnlyPath(configPath)
}

func (c *Command) handleNewSession(ctx context.Context, paths *session.Paths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := framework.RecordSessionCost(ctx, c.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record session cost for backup: %v\n", err)
	}
	retentionDays := session.LoadBackupRetentionDays(*paths)
	if err := session.RotateSession(c.Stdout, *paths, retentionDays); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Session rotation failed: %v\n", err)
	}
}

func (c *Command) setupUIRendering(chatAgent agent.Chatter, cfg *config.Config, opts *cliOptions, logPath string, capturer *input.Capturer) {
	renderer := ui.NewStdUIRenderer(c.SM)
	renderer.SetWriters(c.Stdout, c.Stderr)
	useColor := capturer.IsTTY(c.Stdout) && !opts.rawOutput
	renderer.SetUseColor(useColor)
	subscriber := NewUISubscriber(renderer, cfg.ShowThoughts, cfg.ShowTools, opts.rawOutput, useColor, logPath)
	chatAgent.Subscribe(subscriber.HandleEvent)
}

func (c *Command) applyConfiguration(chatAgent agent.Chatter, cfg *config.Config, opts *cliOptions, paths *session.Paths, pruned int, pData pricing.PricingData, capturer *input.Capturer) {
	c.setupUIRendering(chatAgent, cfg, opts, paths.LogPath, capturer)
	chatAgent.SetLimits(cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns)
	chatAgent.SetTieredThreshold(cfg.ResolveTieredThreshold(pData))
	chatAgent.SetPrunedTurns(pruned)
}

func (c *Command) parseFlags(args []string) (*cliOptions, *flag.FlagSet, error) {
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

func (c *Command) sanitizeArgs(args []string) []string {
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
