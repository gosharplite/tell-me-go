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
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/ui"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
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
	Loader  domain_config.ConfigLoader

	AgentFactory  services.ChatterFactory
	ClientFactory func(cfg *domain_config.Config, pricing domain_pricing.PricingData, bus events.EventBus) (domain_llm.LLMClient, error)
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
		Loader:  &config.YAMLConfigLoader{},
		AgentFactory: func(loader domain_config.ConfigLoader, client domain_llm.LLMGateway, hManager services.HistoryManager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, providerName, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) services.Chatter {
			telemetry.RegisterTraceSubscriber(bus, logPath)

			summarizer := llm.NewSummarizer(client, bus)

			return agent.New(client, hManager, reg, sm, bus, summarizer, providerName,
				agent.WithPricing(model, mode, pricingOverrides),
				agent.WithSessionCostTracker(tracker),
				agent.WithInternalTools(),
				agent.WithLoader(loader),
			)
		},
		ClientFactory: llm.NewClient,
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

	deps, hManager, err := c.buildSessionDependencies(ctx, cfg, opts.configPath, opts.newSession, capturer.(domain_security.UserInteractor))
	if err != nil {
		return err
	}

	defer func() {
		if shutdownErr := deps.GetEventBus().Shutdown(ctx); shutdownErr != nil {
			fmt.Fprintf(c.Stderr, "Warning: Event bus shutdown failed: %v\n", shutdownErr)
		}
	}()

	err = c.performChat(ctx, capturer, opts, prompt, cfg, deps)

	c.finalizeCLI(ctx, hManager, deps, cfg)

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

func (c *chatCommand) performChat(ctx stdctx.Context, capturer orchestration.Capturer, opts *cliOptions, prompt string, cfg *domain_config.Config, deps services.SessionDependencies) error {
	uiRenderer := ui.NewRenderer(c.SM, c.Stdout, c.Stderr)
	historyRenderer := &ui.StdHistoryRenderer{}
	orch := orchestration.NewOrchestrator(c.HomeDir, c.Version, c.Loader, c.SM, c.Stdout, c.Stderr, c.AgentFactory, historyRenderer, uiRenderer)

	sCfg := orchestration.NewSessionConfig(opts.configPath, opts.newSession, opts.lastN, opts.rawOutput, prompt, cfg)
	return orch.Run(ctx, sCfg, deps, capturer)
}

func (c *chatCommand) finalizeCLI(ctx stdctx.Context, hManager services.HistoryManager, deps services.SessionDependencies, cfg *domain_config.Config) {
	// Finalize session
	if saveErr := hManager.Save(ctx); saveErr != nil {
		fmt.Fprintf(c.Stderr, "Warning: Error saving history: %v\n", saveErr)
	}

	// Calculate and record cost
	if recordErr := telemetry.RecordSessionCost(ctx, c.SM, deps.GetTracker(), deps.GetPaths().LogPath, cfg.Model, cfg.Mode, "", deps.GetPricingOverrides()); recordErr != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record final session cost: %v\n", recordErr)
	}
}

func (c *chatCommand) buildSessionDependencies(ctx stdctx.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer domain_security.UserInteractor) (services.SessionDependencies, *history.Manager, error) {
	paths, err := infra_persistence.InitializePaths(c.HomeDir, cfg.Mode)
	if err != nil {
		return nil, nil, err
	}

	pricingOverrides := c.getPricingOverrides(cfg)
	c.setupSecurity(paths, configPath)
	if newSession {
		c.handleNewSession(ctx, paths, cfg, pricingOverrides)
	}

	hManager := history.NewManager(infrapersistence.NewOSFileSystem(), paths.HistoryPath)
	if err := hManager.Load(ctx); err != nil {
		return nil, nil, fmt.Errorf("error loading history: %w", err)
	}

	bus := events.NewSimpleEventBus()

	pricingData := telemetry.GetPricing(ctx, c.SM, filepath.Join(c.HomeDir, "output"))

	client, err := c.ClientFactory(cfg, pricingData, bus)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating client: %w", err)
	}

	gw, ok := client.(domain_llm.LLMGateway)
	if !ok {
		return nil, nil, fmt.Errorf("client does not implement LLMGateway")
	}

	reg := registry.New()

	executor := &exec.RealExecutor{}
	var sessionProvider services.ISessionProvider
	if state, err := infra_persistence.NewSessionState(ctx, paths.ModeDir); err == nil {
		sessionProvider = state
		// Inject model and provider ground truth for tools like get_session_info
		info := state.GetInfo()
		info.Model = cfg.Model
		info.Provider = cfg.SelectedProvider
		state.SetInfo(info)
	} else {
		fmt.Fprintf(c.Stderr, "Warning: Failed to initialize session state: %v\n", err)
	}
	validator := internal_security.NewCommandValidator(c.SM, capturer)

	tools.RegisterAll(
		reg,
		c.SM,
		executor,
		validator,
		sessionProvider,
		paths.LogPath,
		cfg.Model,
		cfg.Mode,
		pricingOverrides,
		client,
		filepath.Join(c.HomeDir, "assets/generated"),
		bus,
		infrapersistence.NewOSFileSystem(),
	)

	// Infrastructure-specific tool registration
	telemetry.RegisterMetrics(reg, c.SM, paths.LogPath, cfg.Model, cfg.Mode, pricingOverrides)
	if ism, ok := c.SM.(*internal_security.SecurityManager); ok {
		internal_security.RegisterPolicy(reg, ism)
	}

	modelPricing := telemetry.GetModelPricing(cfg.Model, pricingData)
	tracker := telemetry.NewSessionCostTracker(c.SM, paths.LogPath, cfg.Mode, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	deps := orchestration.NewSessionDependencies(paths, hManager, client, gw, reg, tracker, pricingData, pricingOverrides, bus)

	return deps, hManager, nil
}

func (c *chatCommand) getPricingOverrides(cfg *domain_config.Config) map[string]domain_pricing.ModelPricing {
	pricingOverrides := make(map[string]domain_pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (c *chatCommand) setupSecurity(paths *persistence.Paths, configPath string) {
	if sm, ok := c.SM.(*internal_security.SecurityManager); ok {
		sm.SetSafePathsFile(paths.SafePathsPath)
		sm.SetReadOnlyPathsFile(paths.ReadPathsPath)
		sm.SetBypassFile(paths.BypassPath)
		sm.SetCommandsLogFile(paths.CommandsLogPath)
		if err := sm.LoadSafePaths(); err != nil {
			fmt.Fprintf(c.Stderr, "Warning: Failed to load safe paths: %v\n", err)
		}
		if err := sm.LoadReadOnlyPaths(); err != nil {
			fmt.Fprintf(c.Stderr, "Warning: Failed to load read-only paths: %v\n", err)
		}
		sm.LoadBypassState()
		sm.RegisterSafePath(filepath.Join(c.HomeDir, "output"))
		sm.RegisterReadOnlyPath(configPath)
	}
}

func (c *chatCommand) handleNewSession(ctx stdctx.Context, paths *persistence.Paths, cfg *domain_config.Config, pricingOverrides map[string]domain_pricing.ModelPricing) {
	timestamp := time.Now().Format("20060102_150405")
	uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(paths.LogPath))
	if err := telemetry.RecordSessionCost(ctx, c.SM, nil, paths.LogPath, cfg.Model, cfg.Mode, uniqueID, pricingOverrides); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Failed to record session cost for backup: %v\n", err)
	}
	retentionDays := infra_persistence.LoadBackupRetentionDays(*paths)
	if err := infra_persistence.RotateSession(c.Stdout, *paths, retentionDays); err != nil {
		fmt.Fprintf(c.Stderr, "Warning: Session rotation failed: %v\n", err)
	}
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
