// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
)

// App represents the tell-me-go application.
type App struct {
	Version       string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	AgentFactory  func(client *api.Client, hManager *history.Manager, registry *tools.Registry, sm *tools.SecurityManager) agent.Chatter
	ClientFactory func(cfg *config.Config, pricing tools.PricingData) (*api.Client, error)
	// Internal properties for better testability
	homeDir string
	sm      *tools.SecurityManager
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
		ClientFactory: func(cfg *config.Config, pricing tools.PricingData) (*api.Client, error) {
			authenticator := &auth.VertexAuth{}
			return api.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, pricing.ThinkingBudgets, cfg.Person, cfg.UseSearch)
		},
	}
}

// Run executes the application logic.
func (a *App) Run(args []string) error {
	// 1. Pre-process args to handle "-l" as a boolean flag that defaults to "-l 1"
	args = a.sanitizeArgs(args)

	// 2. Parse Flags
	opts, fs, err := a.parseFlags(args[1:])
	if err != nil {
		return err
	}

	if opts.showVersion {
		fmt.Fprintf(a.Stdout, "tell-me-go version %s\n", a.Version)
		return nil
	}

	// 2. Load Config
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %v", opts.configPath, err)
	}

	// 3. Initialize Paths
	homeDir := a.homeDir

	sessionName := cfg.Mode
	modeDir := filepath.Join(homeDir, "output", sessionName)
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory [%s]: %v", modeDir, err)
	}

	historyPath := filepath.Join(modeDir, "history.json")
	logPath := filepath.Join(modeDir, "tokens.log")
	commandsLogPath := filepath.Join(modeDir, "commands.log")
	safePathsPath := filepath.Join(modeDir, "safepaths.json")
	readPathsPath := filepath.Join(modeDir, "readpaths.json")
	bypassPath := filepath.Join(modeDir, "bypass.log")
	persistentConfigPath := filepath.Join(modeDir, "config.json")

	// 5. Initialize Components
	a.sm.SetSafePathsFile(safePathsPath)
	a.sm.SetReadOnlyPathsFile(readPathsPath)
	a.sm.SetBypassFile(bypassPath)
	a.sm.SetCommandsLogFile(commandsLogPath)
	if err := a.sm.LoadSafePaths(); err != nil {
		log.Printf("Warning: Failed to load persistent safe paths: %v", err)
	}
	if err := a.sm.LoadReadOnlyPaths(); err != nil {
		log.Printf("Warning: Failed to load persistent read-only paths: %v", err)
	}
	a.sm.LoadBypassState()
	a.sm.RegisterSafePath(filepath.Join(homeDir, "output"))
	a.sm.RegisterReadOnlyPath(opts.configPath)

	if opts.newSession {
		timestamp := time.Now().Format("20060102_150405")
		// Record cost with a unique ID including the timestamp before archiving
		uniqueID := fmt.Sprintf("backup/%s/%s", timestamp, filepath.Base(logPath))
		_ = tools.RecordSessionCost(a.sm, logPath, cfg.Model, cfg.Mode, uniqueID)
		a.archiveSessionFilesWithTimestamp(homeDir, timestamp, historyPath, logPath, commandsLogPath)
		a.cleanupOldBackups(homeDir, cfg.Mode)
	}

	hManager := history.NewManager(historyPath)
	if err := hManager.Load(); err != nil {
		return fmt.Errorf("error loading history: %v", err)
	}
	// Proactively prune history immediately after loading to ensure cache efficiency.
	// We prune down to 50% of the limit to provide a stable cache prefix for the next turns.
	pruned := hManager.Prune(cfg.MaxHistoryTurns)

	if opts.lastN > 0 {
		a.showHistory(hManager, opts.lastN, opts.rawOutput)
	}

	// 4. Handle Prompt
	prompt, err := a.capturePrompt(fs, opts.lastN)
	if err != nil {
		return err
	}

	// If the user only requested history (-l), exit after displaying it.
	if prompt == "" && opts.lastN > 0 {
		return nil
	}

	pricing := tools.GetPricing(context.Background(), filepath.Join(homeDir, "output"))

	// Load persistent config to augment system prompt (e.g., smart_suggestions)
	if data, err := os.ReadFile(persistentConfigPath); err == nil {
		var pCfg map[string]string
		if err := json.Unmarshal(data, &pCfg); err == nil {
			if pCfg["smart_suggestions"] == "on" {
				cfg.Person += "\n\nUX Preference: smart_suggestions is ENABLED. You MUST conclude every response by suggesting 2 to 3 context-aware follow-up commands (tool calls or workflow actions) relevant to the current conversation state. If the AI detects a repeating command pattern, it should increase the suggestion count."
			}
		} else {
			fmt.Fprintf(a.Stderr, "Warning: Failed to parse persistent config [%s]: %v\n", persistentConfigPath, err)
		}
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(a.Stderr, "Warning: Failed to read persistent config [%s]: %v\n", persistentConfigPath, err)
	}

	hManager.Snapshot()

	client, err := a.ClientFactory(cfg, pricing)
	if err != nil {
		return fmt.Errorf("error creating client: %v", err)
	}

	registry := tools.NewRegistry()
	tools.RegisterFileSystemTools(registry, a.sm)
	tools.RegisterIntelligenceTools(registry, a.sm)
	tools.RegisterSystemTools(registry, a.sm)
	tools.RegisterGitTools(registry, a.sm)
	tools.RegisterDevTools(registry, a.sm)
	tools.RegisterTeamsTools(registry, a.sm)
	tools.RegisterStateTools(registry, homeDir, hManager, cfg.Mode, a.sm)
	tools.RegisterMetricsTools(registry, a.sm, logPath, cfg.Model, cfg.Mode)
	tools.RegisterMediaTools(registry, a.sm, client)

	// 6. Execute Agent
	chatAgent := a.AgentFactory(client, hManager, registry, a.sm)
	chatAgent.SetLogFile(logPath)
	chatAgent.SetUIOptions(cfg.ShowThoughts, cfg.ShowTools)
	chatAgent.SetRawOutput(opts.rawOutput)
	chatAgent.SetLimits(cfg.MaxToolTurns, cfg.MaxHistoryTokens, cfg.MaxHistoryTurns)
	chatAgent.SetPrunedTurns(pruned)
	chatAgent.SetConcurrency(cfg.MaxConcurrentTools, cfg.ToolTimeoutSeconds)

	if err := chatAgent.Chat(prompt); err != nil {
		return fmt.Errorf("error: %v", err)
	}

	// 7. Save History
	if err := hManager.Save(); err != nil {
		return fmt.Errorf("error saving history: %v", err)
	}

	// 8. Record session cost
	if err := tools.RecordSessionCost(a.sm, logPath, cfg.Model, cfg.Mode, ""); err != nil {
		fmt.Fprintf(a.Stderr, "Warning: Failed to record final session cost: %v\n", err)
	}

	return nil
}
