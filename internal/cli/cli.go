// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
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
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

// App represents the tell-me-go application.
type App struct {
	Version       string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	AgentFactory  func(client *api.Client, hManager *history.Manager, registry *registry.Registry, sm *security.SecurityManager, disableStreaming bool) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing llm.PricingData) (*api.Client, error)
	// Internal properties for better testability
	homeDir string
	sm      *security.SecurityManager
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

	sm := security.NewSecurityManager(os.Stdin)

	return &App{
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		homeDir: homeDir,
		sm:      sm,
		AgentFactory: func(client *api.Client, hManager *history.Manager, reg *registry.Registry, sm *security.SecurityManager, disableStreaming bool) agent.Chatter {
			return agent.New(client, hManager, reg, sm, disableStreaming)
		},
		ClientFactory: func(cfg *config.Config, pricing llm.PricingData) (*api.Client, error) {
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

	// 1. Parse Flags & Load Config
	args = a.sanitizeArgs(args)
	opts, fs, err := a.parseFlags(args[1:])
	if err != nil {
		return err
	}
	if opts.showVersion {
		fmt.Fprintf(a.Stdout, "tell-me-go version %s\n", a.Version)
		return nil
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %w", opts.configPath, err)
	}

	// 2. Initialize Paths & Persistent Config
	paths, err := a.initPaths(cfg)
	if err != nil {
		return err
	}

	_, err = a.loadPersistentConfig(paths, cfg)
	if err != nil {
		log.Printf("Warning: Failed to load/update persistent config: %v", err)
	}

	// 3. Initialize Security & Session
	a.setupSecurity(paths, opts, cfg)

	pricingOverrides := make(map[string]llm.ModelPricing)
	for k, v := range cfg.Models {
		if v.Pricing.Comp > 0 {
			pricingOverrides[k] = v.Pricing
		}
	}

	if opts.newSession {
		a.handleNewSession(paths, cfg, pricingOverrides)
	}

	// 4. Initialize History
	hManager := history.NewManager(paths.historyPath)
	if err := hManager.Load(ctx); err != nil {
		return fmt.Errorf("error loading history: %w", err)
	}
	pruned, _ := hManager.Prune(ctx, cfg.MaxHistoryTurns)

	if opts.lastN > 0 {
		a.showHistory(hManager, opts.lastN, opts.rawOutput)
	}

	// 5. Handle Prompt
	prompt, err := a.capturePrompt(ctx, fs, opts.lastN)
	if err != nil {
		return err
	}
	if prompt == "" && opts.lastN > 0 {
		return nil
	}

	// 6. Setup Agent & Client
	pricing := framework.GetPricing(ctx, a.sm, filepath.Join(a.homeDir, "output"))

	hManager.Snapshot()

	client, err := a.ClientFactory(cfg, pricing)
	if err != nil {
		return fmt.Errorf("error creating client: %w", err)
	}

	registry := a.setupRegistry(client, cfg, paths, pricingOverrides)

	chatAgent := a.AgentFactory(client, hManager, registry, a.sm, cfg.DisableStreaming)
	a.applyConfiguration(chatAgent, cfg, opts, paths, pruned, pricing)

	// 7. Execute & Finalize
	sess := agent.NewSession(hManager)
	sess.PrunedTurns = pruned

	if err := chatAgent.Chat(ctx, sess, prompt); err != nil {
		return fmt.Errorf("error: %w", err)
	}

	if err := hManager.Save(ctx); err != nil {
		return fmt.Errorf("error saving history: %w", err)
	}

	if err := framework.RecordSessionCost(ctx, a.sm, paths.logPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(a.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}

	return nil
}
