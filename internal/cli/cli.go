// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
	Version string
}

// New creates a new App instance.
func New(version string) *App {
	return &App{
		Version: version,
	}
}

// Run executes the application logic.
func (a *App) Run() {
	// 1. Define and Parse Flags
	configPath := flag.String("c", "configs/vertex.yaml", "Path to the configuration file")
	newSession := flag.Bool("new", false, "Start a new session")
	showVersion := flag.Bool("v", false, "Show version information")
	lastN := flag.Int("l", 0, "Show the last N messages from history")
	flag.Parse()

	if *showVersion {
		fmt.Printf("tell-me-go version %s\n", a.Version)
		os.Exit(0)
	}

	// 2. Load Config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Error loading config [%s]: %v", *configPath, err)
	}

	// 3. Initialize Paths
	homeDir := os.Getenv("TELL_ME_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("AIT_HOME")
	}
	if homeDir == "" {
		homeDir = "."
	}

	sessionName := "last-" + cfg.Mode
	historyPath := filepath.Join(homeDir, "output", sessionName+".json")
	logPath := historyPath + ".log"
	safePathsPath := filepath.Join(homeDir, "output", sessionName+".safepaths.json")

	if *newSession {
		a.archiveSessionFiles(homeDir, sessionName, historyPath, logPath)
	}

	hManager := history.NewManager(historyPath)
	if err := hManager.Load(); err != nil {
		log.Fatalf("Error loading history: %v", err)
	}

	if *lastN > 0 {
		a.showHistory(hManager, *lastN)
	}

	// 4. Handle Prompt
	prompt := a.capturePrompt(*lastN)

	// 5. Initialize Components
	tools.SetSafePathsFile(safePathsPath)
	if err := tools.LoadSafePaths(); err != nil {
		log.Printf("Warning: Failed to load persistent safe paths: %v", err)
	}
	tools.RegisterSafePath(filepath.Join(homeDir, "output"))
	tools.RegisterSafePath(*configPath)

	hManager.Snapshot()

	authenticator := &auth.VertexAuth{}
	client, err := api.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, cfg.Person, cfg.UseSearch)
	if err != nil {
		log.Fatalf("Error creating client: %v", err)
	}

	registry := tools.NewRegistry()
	tools.RegisterFileSystemTools(registry)
	tools.RegisterIntelligenceTools(registry)
	tools.RegisterSystemTools(registry)
	tools.RegisterGitTools(registry)
	tools.RegisterDevTools(registry)
	tools.RegisterStateTools(registry, homeDir, hManager)
	tools.RegisterMetricsTools(registry, logPath, cfg.Model)
	tools.RegisterMediaTools(registry, client)

	// 6. Execute Agent
	chatAgent := agent.New(client, hManager, registry)
	chatAgent.SetLogFile(logPath)
	chatAgent.SetUIOptions(cfg.ShowThoughts, cfg.ShowTools)
	chatAgent.SetLimits(cfg.MaxToolTurns, cfg.MaxHistoryTokens)
	chatAgent.SetConcurrency(cfg.MaxConcurrentTools, cfg.ToolTimeoutSeconds)

	if err := chatAgent.Chat(prompt); err != nil {
		log.Fatalf("Error: %v", err)
	}

	// 7. Save History
	hManager.Prune(cfg.MaxHistoryTurns)
	if err := hManager.Save(); err != nil {
		log.Fatalf("Error saving history: %v", err)
	}
}

func (a *App) capturePrompt(lastN int) string {
	prompt := flag.Arg(0)
	stat, _ := os.Stdin.Stat()
	isTerminal := (stat.Mode() & os.ModeCharDevice) != 0

	if !isTerminal {
		b, err := io.ReadAll(os.Stdin)
		if err == nil && len(b) > 0 {
			if prompt != "" {
				prompt = prompt + "\n" + string(b)
			} else {
				prompt = string(b)
			}
		}
	} else if prompt == "" && lastN == 0 {
		fmt.Println("\033[0;33m[Reading multi-line input. Press Ctrl+D to send]\033[0m")
		b, err := io.ReadAll(os.Stdin)
		if err == nil {
			prompt = string(b)
		}
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		if lastN > 0 {
			os.Exit(0)
		}
		fmt.Println("Usage: tell-me-go [flags] <prompt>")
		flag.PrintDefaults()
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\033[0;32m[%s] Input captured. Processing...\033[0m\n", time.Now().Format("15:04:05"))
	return prompt
}

func (a *App) showHistory(hManager *history.Manager, n int) {
	contents := hManager.GetContents()
	if len(contents) == 0 {
		fmt.Println("No history found.")
		return
	}

	if n > len(contents) {
		n = len(contents)
	}

	start := len(contents) - n
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)

	for i := start; i < len(contents); i++ {
		c := contents[i]
		roleColor := "\033[1;34m" // Blue for User
		if c.Role != "user" {
			roleColor = "\033[1;35m" // Magenta for Model
		}
		fmt.Printf("%s[%s]%s\n", roleColor, strings.ToUpper(c.Role), "\033[0m")
		for _, p := range c.Parts {
			if p.Text != "" {
				out, err := r.Render(p.Text)
				if err != nil {
					fmt.Println(p.Text)
				} else {
					fmt.Print(out)
				}
			}
			if p.FunctionCall != nil {
				fmt.Printf("\033[0;36m[Tool Call] %s\033[0m\n", p.FunctionCall.Name)
			}
			if p.FunctionResponse != nil {
				fmt.Printf("\033[0;36m[Tool Response] %s\033[0m\n", p.FunctionResponse.Name)
			}
		}
		fmt.Println()
	}
}

func (a *App) archiveSessionFiles(homeDir, sessionName, historyPath, logPath string) {
	timestamp := time.Now().Format("20060102_150405")
	backupDir := filepath.Join(homeDir, "output", "backups", timestamp)

	filesToMove := []string{
		historyPath,
		logPath,
	}

	backupCreated := false
	for _, f := range filesToMove {
		if _, err := os.Stat(f); err == nil {
			if !backupCreated {
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					fmt.Fprintf(os.Stderr, "Error creating backup directory: %v\n", err)
					return
				}
				fmt.Printf("Archiving existing session files to %s\n", backupDir)
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := os.Rename(f, dest); err != nil {
				fmt.Fprintf(os.Stderr, "Error archiving %s: %v\n", f, err)
			}
		}
	}
}
