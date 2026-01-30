// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
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

	// 2. Define and Parse Flags
	fs := flag.NewFlagSet("tell-me-go", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)

	configPath := fs.String("c", "configs/vertex.yaml", "Path to the configuration file")
	newSession := fs.Bool("new", false, "Start a new session")
	showVersion := fs.Bool("v", false, "Show version information")
	lastN := fs.Int("l", 0, "Show the last N messages from history")
	rawOutput := fs.Bool("r", false, "Show raw output (without markdown rendering)")

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintf(a.Stdout, "tell-me-go version %s\n", a.Version)
		return nil
	}

	// 2. Load Config
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("error loading config [%s]: %v", *configPath, err)
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
	a.sm.RegisterReadOnlyPath(*configPath)

	if *newSession {
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

	if *lastN > 0 {
		a.showHistory(hManager, *lastN, *rawOutput)
	}

	// 4. Handle Prompt
	prompt, err := a.capturePrompt(fs, *lastN)
	if err != nil {
		return err
	}

	// If the user only requested history (-l), exit after displaying it.
	if prompt == "" && *lastN > 0 {
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
	chatAgent.SetRawOutput(*rawOutput)
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

func (a *App) capturePrompt(fs *flag.FlagSet, lastN int) (string, error) {
	prompt := strings.Join(fs.Args(), " ")
	var isTerminal bool
	if f, ok := a.Stdin.(*os.File); ok {
		stat, _ := f.Stat()
		isTerminal = (stat.Mode() & os.ModeCharDevice) != 0
	} else {
		isTerminal = false // Assume non-terminal for non-file readers (like buffers in tests)
	}

	if !isTerminal {
		b, err := io.ReadAll(a.Stdin)
		if err == nil && len(b) > 0 {
			if prompt != "" {
				prompt = prompt + "\n" + string(b)
			} else {
				prompt = string(b)
			}
		}
	} else if prompt == "" && lastN == 0 {
		fmt.Fprintln(a.Stdout, "\033[0;33m[Reading multi-line input. Press Ctrl+D to send]\033[0m")
		b, err := io.ReadAll(a.Stdin)
		if err == nil {
			prompt = string(b)
		}
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if lastN > 0 {
			return "", nil // Valid case if just showing history
		}
		fmt.Fprintln(a.Stderr, "Usage: tell-me-go [flags] <prompt>")
		fs.SetOutput(a.Stderr)
		fs.PrintDefaults()
		return "", fmt.Errorf("empty prompt")
	}
	a.sm.TerminalLock()
	fmt.Fprintf(a.Stderr, "\033[0;32m[%s] Input captured. Processing...\033[0m\n", time.Now().Format("15:04:05"))
	a.sm.TerminalUnlock()
	return prompt, nil
}

func (a *App) showHistory(hManager *history.Manager, n int, raw bool) {
	contents := hManager.GetContents()
	if len(contents) == 0 {
		fmt.Fprintln(a.Stdout, "No history found.")
		return
	}

	if n > len(contents) {
		n = len(contents)
	}

	start := len(contents) - n
	var r *glamour.TermRenderer
	if !raw {
		r, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithEmoji(),
		)
	}

	for i := start; i < len(contents); i++ {
		c := contents[i]
		roleColor := "\033[1;34m" // Blue for User
		if c.Role != "user" {
			roleColor = "\033[1;35m" // Magenta for Model
		}
		fmt.Fprintf(a.Stdout, "%s[%s]%s\n", roleColor, strings.ToUpper(c.Role), "\033[0m")
		for _, p := range c.Parts {
			if p.Text != "" {
				if raw || r == nil {
					fmt.Fprint(a.Stdout, p.Text)
					if !strings.HasSuffix(p.Text, "\n") {
						fmt.Fprintln(a.Stdout)
					}
				} else {
					out, err := r.Render(p.Text)
					if err != nil {
						fmt.Fprintln(a.Stdout, p.Text)
					} else {
						fmt.Fprint(a.Stdout, out)
					}
				}
			}
			if p.FunctionCall != nil {
				fmt.Fprintf(a.Stdout, "\033[0;36m[Tool Call] %s\033[0m\n", p.FunctionCall.Name)
			}
			if p.FunctionResponse != nil {
				fmt.Fprintf(a.Stdout, "\033[0;36m[Tool Response] %s\033[0m\n", p.FunctionResponse.Name)
			}
		}
		fmt.Fprintln(a.Stdout)
	}
}

func (a *App) archiveSessionFilesWithTimestamp(homeDir, timestamp string, filesToMove ...string) {
	backupDir := filepath.Join(homeDir, "output", "backups", timestamp)

	backupCreated := false
	for _, f := range filesToMove {
		if _, err := os.Stat(f); err == nil {
			if !backupCreated {
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					a.sm.TerminalLock()
					fmt.Fprintf(a.Stderr, "Error creating backup directory: %v\n", err)
					a.sm.TerminalUnlock()
					return
				}
				fmt.Fprintf(a.Stdout, "Archiving existing session files to %s\n", backupDir)
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := os.Rename(f, dest); err != nil {
				a.sm.TerminalLock()
				fmt.Fprintf(a.Stderr, "Error archiving %s: %v\n", f, err)
				a.sm.TerminalUnlock()
			}
		}
	}
}

func (a *App) cleanupOldBackups(homeDir, mode string) {
	backupBaseDir := filepath.Join(homeDir, "output", "backups")
	entries, err := os.ReadDir(backupBaseDir)
	if err != nil {
		return // Likely doesn't exist yet
	}

	retentionDays := 30
	configPath := filepath.Join(homeDir, "output", mode, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg map[string]string
		if err := json.Unmarshal(data, &cfg); err == nil {
			if val, ok := cfg["backup_retention_days"]; ok {
				if days, err := strconv.Atoi(val); err == nil {
					retentionDays = days
				}
			}
		}
	}

	if retentionDays <= 0 {
		return // 0 or negative means keep forever
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Format: YYYYMMDD_HHMMSS (15 chars)
		if len(entry.Name()) < 15 {
			continue
		}

		folderTime, err := time.Parse("20060102_150405", entry.Name()[:15])
		if err != nil {
			continue
		}

		if folderTime.Before(cutoff) {
			path := filepath.Join(backupBaseDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				a.sm.TerminalLock()
				fmt.Fprintf(a.Stderr, "Warning: Failed to cleanup old backup %s: %v\n", path, err)
				a.sm.TerminalUnlock()
			}
		}
	}
}

func (a *App) sanitizeArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}

	processed := args[1:]
	for i, arg := range processed {
		if arg == "-l" {
			// If -l is the last argument or the next argument is not a number,
			// it means the user didn't provide a specific count for -l.
			isNextNum := false
			if i+1 < len(processed) {
				if _, err := strconv.Atoi(processed[i+1]); err == nil {
					isNextNum = true
				}
			}

			if !isNextNum {
				// Insert "1" after "-l" to satisfy the integer flag
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
