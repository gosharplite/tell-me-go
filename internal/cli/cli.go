// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
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
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// App represents the tell-me-go application.
type App struct {
	Version       string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	AgentFactory  func(client *api.Client, hManager *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing types.PricingData) (*api.Client, error)
	// Internal properties for better testability
	homeDir string
	sm      *tools.SecurityManager
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

	sm := tools.NewSecurityManager()

	return &App{
		Version: version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		homeDir: homeDir,
		sm:      sm,
		AgentFactory: func(client *api.Client, hManager *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) agent.Chatter {
			return agent.New(client, hManager, registry, sm)
		},
		ClientFactory: func(cfg *config.Config, pricing types.PricingData) (*api.Client, error) {
			authenticator := &auth.VertexAuth{}
			maxBudget := cfg.ResolveThinkingBudget(cfg.Model, pricing)
			return api.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch)
		},
	}
}

// Run executes the application logic.
func (a *App) Run(args []string) error {
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
		return fmt.Errorf("error loading config [%s]: %v", opts.configPath, err)
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

	pricingOverrides := make(map[string]types.ModelPricing)
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
	if err := hManager.Load(); err != nil {
		return fmt.Errorf("error loading history: %v", err)
	}
	pruned, _ := hManager.Prune(cfg.MaxHistoryTurns)

	if opts.lastN > 0 {
		a.showHistory(hManager, opts.lastN, opts.rawOutput)
	}

	// 5. Handle Prompt
	prompt, err := a.capturePrompt(fs, opts.lastN)
	if err != nil {
		return err
	}
	if prompt == "" && opts.lastN > 0 {
		return nil
	}

	// 6. Setup Agent & Client
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pricing := tools.GetPricing(ctx, a.sm, filepath.Join(a.homeDir, "output"))

	hManager.Snapshot()

	client, err := a.ClientFactory(cfg, pricing)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}

	registry := a.setupRegistry(client, cfg, paths, pricingOverrides)

	chatAgent := a.AgentFactory(client, hManager, registry, a.sm)
	a.configureAgent(chatAgent, cfg, opts, paths, pruned)

	// 7. Execute & Finalize
	if err := chatAgent.Chat(ctx, prompt); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	if err := hManager.Save(); err != nil {
		return fmt.Errorf("error saving history: %v", err)
	}

	if err := tools.RecordSessionCost(ctx, a.sm, paths.logPath, cfg.Model, cfg.Mode, "", pricingOverrides); err != nil {
		fmt.Fprintf(a.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}

	return nil
}
