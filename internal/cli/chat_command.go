// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
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
	SM      *internal_security.SecurityManager

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

type sessionDeps struct {
	paths            *persistence.Paths
	hManager         *history.Manager
	client           *llm.Client
	registry         domaintools.IToolRegistry
	tracker          domain_pricing.ICostTracker
	pData            domain_pricing.PricingData
	pricingOverrides map[string]domain_pricing.ModelPricing
	bus              events.EventBus
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

func (c *chatCommand) prepareSession(ctx stdctx.Context, cfg *config.Config, opts *cliOptions) (*sessionDeps, error) {
	paths, err := persistence.InitializePaths(c.HomeDir, cfg.Mode)
	if err != nil {
		return nil, err
	}

	pricingOverrides := c.getPricingOverrides(cfg)
	c.setupSecurity(paths, opts.configPath)
	if opts.newSession {
		c.handleNewSession(ctx, paths, cfg, pricingOverrides)
	}

	return c.initializeDependencies(ctx, paths, cfg, pricingOverrides)
}

func (c *chatCommand) renderHistory(hManager *history.Manager, opts *cliOptions, cfg *config.Config, isTTY bool) {
	if opts.lastN <= 0 {
		return
	}
	ui.History(c.Stdout, hManager, opts.lastN, ui.RenderOptions{
		Raw:          opts.rawOutput,
		ShowThoughts: cfg.ShowThoughts,
		UseColor:     isTTY && !opts.rawOutput,
	})
}

// Execute runs the chat command logic.
func (c *chatCommand) Execute(ctx stdctx.Context, args []string) error {
	capturer := ui.NewCapturer(c.Stdin, c.Stdout, c.Stderr, c.SM)
	c.SM.SetInteractor(capturer)

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

	deps, err := c.prepareSession(ctx, cfg, opts)
	if err != nil {
		return err
	}
	defer func() {
		if err := deps.bus.Shutdown(ctx); err != nil {
			fmt.Fprintf(c.Stderr, "Warning: Failed to shutdown event bus: %v\n", err)
		}
	}()

	c.renderHistory(deps.hManager, opts, cfg, capturer.IsTTY(c.Stdout))
	if prompt == "" && opts.lastN > 0 {
		return nil
	}

	chatAgent := c.AgentFactory(deps.client, deps.hManager, deps.registry, c.SM, cfg.DisableStreaming, deps.bus, cfg.Model, cfg.Mode, deps.paths.LogPath, deps.pricingOverrides, deps.tracker)
	defer func() {
		if err := chatAgent.Shutdown(ctx); err != nil {
			fmt.Fprintf(c.Stderr, "Warning: Agent shutdown failed: %v\n", err)
		}
	}()

	if err := c.applyConfiguration(ctx, chatAgent, cfg, opts, deps.paths, deps.pData, capturer); err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	sess := orchestration.NewSession(sessionID, deps.hManager)
	if err := chatAgent.Chat(ctx, sess, prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return c.finalizeSession(ctx, chatAgent, deps.hManager, *deps.paths, cfg, deps.pricingOverrides)
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

func (c *chatCommand) initializeDependencies(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) (*sessionDeps, error) {
	hManager := history.NewManager(paths.HistoryPath)
	if err := hManager.Load(ctx); err != nil {
		return nil, fmt.Errorf("error loading history: %w", err)
	}

	bus := events.NewSimpleEventBus()

	pricingData := telemetry.GetPricing(ctx, c.SM, filepath.Join(c.HomeDir, "output"))

	client, err := c.ClientFactory(cfg, pricingData, bus)
	if err != nil {
		return nil, fmt.Errorf("error creating client: %w", err)
	}

	registry := c.setupRegistry(client, cfg, paths, pricingOverrides, bus)
	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	tracker := telemetry.NewSessionCostTracker(c.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	return &sessionDeps{
		paths:            paths,
		hManager:         hManager,
		client:           client,
		registry:         registry,
		tracker:          tracker,
		pData:            pricingData,
		pricingOverrides: pricingOverrides,
		bus:              bus,
	}, nil
}

func (c *chatCommand) finalizeSession(ctx stdctx.Context, chatAgent agent.Chatter, hManager *history.Manager, paths persistence.Paths, cfg *config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) error {
	if err := hManager.Save(ctx); err != nil {
		return fmt.Errorf("error saving history: %w", err)
	}
	if err := telemetry.RecordSessionCost(ctx, c.SM, chatAgent.GetCostTracker(), paths.LogPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}
	return nil
}

func (c *chatCommand) getPricingOverrides(cfg *config.Config) map[string]domain_pricing.ModelPricing {
	pricingOverrides := make(map[string]domain_pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (c *chatCommand) setupSecurity(paths *persistence.Paths, configPath string) {
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

func (c *chatCommand) handleNewSession(ctx stdctx.Context, paths *persistence.Paths, cfg *config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, c.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record session cost for backup: %v\n", err)
	}
	retentionDays := persistence.LoadBackupRetentionDays(*paths)
	if err := persistence.RotateSession(c.Stdout, *paths, retentionDays); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Session rotation failed: %v\n", err)
	}
}

func (c *chatCommand) setupUIRendering(chatAgent agent.Chatter, cfg *config.Config, opts *cliOptions, logPath string, capturer *ui.Capturer) {
	renderer := ui.NewRenderer(c.SM, c.Stdout, c.Stderr)
	useColor := capturer.IsTTY(c.Stdout) && !opts.rawOutput
	renderer.SetUseColor(useColor)
	subscriber := newUISubscriber(renderer, cfg.ShowThoughts, cfg.ShowTools, opts.rawOutput, useColor, logPath)
	chatAgent.Subscribe(subscriber.HandleEvent)
}

func (c *chatCommand) applyConfiguration(ctx stdctx.Context, chatAgent agent.Chatter, cfg *config.Config, opts *cliOptions, paths *persistence.Paths, pData domain_pricing.PricingData, capturer *ui.Capturer) error {
	c.setupUIRendering(chatAgent, cfg, opts, paths.LogPath, capturer)
	if err := chatAgent.SetLimits(ctx, cfg.MaxToolTurns, cfg.ResolveContextWindow(), cfg.MaxHistoryTurns); err != nil {
		return err
	}
	return chatAgent.SetTieredThreshold(ctx, cfg.ResolveTieredThreshold(pData))
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
