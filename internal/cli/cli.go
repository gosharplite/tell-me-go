// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/config"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	internal_security "github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
)

// App represents the tell-me-go application.
type App struct {
	Version       string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	AgentFactory  func(client *api.Client, hManager *history.Manager, registry domaintools.IToolRegistry, sm security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing pricing.PricingData) (*api.Client, error)
	// Internal properties for better testability
	homeDir string
	sm      *internal_security.SecurityManager
}

type sessionPaths struct {
	modeDir              string
	historyPath          string
	logPath              string
	commandsLogPath      string
	safePathsPath        string
	readPathsPath        string
	bypassPath           string
	persistentConfigPath string
}

// New creates a new App instance with default IO and factories.
func New(version string) *App {
	homeDir := os.Getenv("TELL_ME_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("AIT_HOME")
	}
	if homeDir == "" {
		homeDir = "."
	}

	sm := internal_security.NewSecurityManager(os.Stdin)

	return &App{
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		homeDir: homeDir,
		sm:      sm,
		AgentFactory: func(client *api.Client, hManager *history.Manager, reg domaintools.IToolRegistry, sm security.ISecurityManager, disableStreaming bool, model, mode string, pricingOverrides map[string]pricing.ModelPricing, tracker domain_pricing.ICostTracker) agent.Chatter {
			return agent.New(client, hManager, reg, sm, disableStreaming,
				agent.WithPricing(model, mode, pricingOverrides),
				agent.WithSessionCostTracker(tracker),
			)
		},
		ClientFactory: func(cfg *config.Config, pricing pricing.PricingData) (*api.Client, error) {
			authenticator := &auth.VertexAuth{}
			maxBudget := cfg.ResolveThinkingBudget(cfg.Model, pricing)
			return api.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch)
		},
	}
}

// Run executes the application logic.
func (a *App) Run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := a.run(ctx, args); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(a.Stderr)
			return nil
		}
		return err
	}
	return nil
}

func (a *App) run(ctx context.Context, args []string) error {
	// Sync security manager with current app stdin
	a.sm.SetInputReader(a.Stdin)

	// 1. Initialize Configuration
	opts, fs, cfg, err := a.initializeConfiguration(args)
	if err != nil {
		return err
	}
	if opts.showVersion {
		return nil
	}

	// 2. Handle Prompt Early
	// We capture the prompt before creating any directories or persistent configs
	// to ensure that if the prompt is empty (and not just showing history),
	// we exit before making any changes to the filesystem.
	prompt, err := a.capturePrompt(ctx, fs, opts.lastN)
	if err != nil {
		return err
	}

	// 3. Prepare Session Paths
	paths, err := a.prepareSessionPaths(cfg)
	if err != nil {
		return err
	}

	// 4. Initialize Security & Session
	pricingOverrides := a.getPricingOverrides(cfg)
	a.setupSessionState(&paths, opts, cfg, pricingOverrides)

	// 5. Initialize Dependencies
	hManager, client, registry, tracker, pruned, pricing, err := a.initializeDependencies(ctx, paths, cfg, pricingOverrides)
	if err != nil {
		return err
	}

	// 6. Handle History & Early Exit
	if a.handleHistoryAndExit(prompt, opts, hManager, cfg) {
		return nil
	}

	// 7. Setup Agent & Execute
	chatAgent := a.AgentFactory(client, hManager, registry, a.sm, cfg.DisableStreaming, cfg.Model, cfg.Mode, pricingOverrides, tracker)
	a.applyConfiguration(chatAgent, cfg, opts, &paths, pruned, pricing)

	sess := agent.NewSession(hManager)
	sess.PrunedTurns = pruned

	if err := chatAgent.Chat(ctx, sess, prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	return a.finalizeSession(ctx, chatAgent, hManager, paths, cfg, pricingOverrides)
}

func (a *App) initializeConfiguration(args []string) (*cliOptions, *flag.FlagSet, *config.Config, error) {
	args = a.sanitizeArgs(args)
	opts, fs, err := a.parseFlags(args[1:])
	if err != nil {
		return nil, nil, nil, err
	}
	if opts.showVersion {
		fmt.Fprintf(a.Stdout, "tell-me-go version %s\n", a.Version)
		return opts, fs, nil, nil
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	return opts, fs, cfg, nil
}

func (a *App) prepareSessionPaths(cfg *config.Config) (sessionPaths, error) {
	paths, err := a.initPaths(cfg)
	if err != nil {
		return sessionPaths{}, err
	}

	_, err = a.loadPersistentConfig(paths, cfg)
	if err != nil {
		log.Printf("Warning: Failed to load/update persistent config: %v", err)
	}

	return *paths, nil
}

func (a *App) initializeDependencies(ctx context.Context, paths sessionPaths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) (*history.Manager, *api.Client, domaintools.IToolRegistry, domain_pricing.ICostTracker, int, pricing.PricingData, error) {
	hManager := history.NewManager(paths.historyPath)
	if err := hManager.Load(ctx); err != nil {
		return nil, nil, nil, nil, 0, pricing.PricingData{}, fmt.Errorf("error loading history: %w", err)
	}
	pruned, _ := hManager.Prune(ctx, cfg.MaxHistoryTurns)

	pricingData := framework.GetPricing(ctx, a.sm, filepath.Join(a.homeDir, "output"))

	hManager.Snapshot()

	client, err := a.ClientFactory(cfg, pricingData)
	if err != nil {
		return nil, nil, nil, nil, 0, pricingData, fmt.Errorf("error creating client: %w", err)
	}

	registry := a.setupRegistry(client, cfg, &paths, pricingOverrides)

	modelPricing := framework.GetModelPricing(cfg.Model, pricingData)
	tracker := framework.NewSessionCostTracker(a.sm, paths.logPath, cfg.Model, modelPricing, pricingData)
	tracker.Warmup()

	return hManager, client, registry, tracker, pruned, pricingData, nil
}

func (a *App) finalizeSession(ctx context.Context, chatAgent agent.Chatter, hManager *history.Manager, paths sessionPaths, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) error {
	if err := hManager.Save(ctx); err != nil {
		return fmt.Errorf("error saving history: %w", err)
	}

	if err := framework.RecordSessionCost(ctx, a.sm, chatAgent.GetCostTracker(), paths.logPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(a.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}

	return nil
}

func (a *App) getPricingOverrides(cfg *config.Config) map[string]pricing.ModelPricing {
	pricingOverrides := make(map[string]pricing.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}
	return pricingOverrides
}

func (a *App) setupSessionState(paths *sessionPaths, opts *cliOptions, cfg *config.Config, pricingOverrides map[string]pricing.ModelPricing) {
	a.setupSecurity(paths, opts, cfg)
	if opts.newSession {
		a.handleNewSession(paths, cfg, pricingOverrides)
	}
}

func (a *App) handleHistoryAndExit(prompt string, opts *cliOptions, hManager *history.Manager, cfg *config.Config) bool {
	if opts.lastN > 0 {
		a.showHistory(hManager, opts.lastN, opts.rawOutput, cfg.ShowThoughts)
	}

	return prompt == "" && opts.lastN > 0
}
