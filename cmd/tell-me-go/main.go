// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
)

const Version = "0.8.0"

func main() {
	// 1. Define Flags
	configPath := flag.String("c", "configs/vertex.yaml", "Path to the configuration file")
	newSession := flag.Bool("new", false, "Start a new session")
	showVersion := flag.Bool("v", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("tell-me-go version %s\n", Version)
		os.Exit(0)
	}

	// 2. Handle Prompt Argument
	prompt := flag.Arg(0)
	if prompt == "" {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			var b []byte
			b, err := os.ReadFile(os.Stdin.Name())
			if err == nil {
				prompt = string(b)
			}
		} else {
			fmt.Println("\033[0;33m[Reading multi-line input. Press Ctrl+D to send]\033[0m")
			var sb strings.Builder
			var buf [1024]byte
			for {
				n, err := os.Stdin.Read(buf[:])
				if n > 0 {
					sb.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
			prompt = sb.String()
		}
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		fmt.Println("Usage: tell-me-go [flags] <prompt>")
		flag.PrintDefaults()
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\033[0;32m[%s] > %s\033[0m\n", time.Now().Format("15:04:05"), prompt)

	// 3. Load Config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Error loading config [%s]: %v", *configPath, err)
	}

	// 4. Initialize Authentication
	authenticator := &auth.VertexAuth{}

	// 5. Initialize Components
	// Determine Home Directory (Compatibility with Bash AIT_HOME)
	homeDir := os.Getenv("TELL_ME_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("AIT_HOME")
	}
	if homeDir == "" {
		homeDir = "."
	}

	// Extract session name from config file (e.g., configs/vertex.yaml -> vertex)
	// This matches the Bash version's logic in setup_session.
	configBase := filepath.Base(*configPath)
	sessionName := strings.TrimSuffix(configBase, filepath.Ext(configBase))
	historyPath := filepath.Join(homeDir, "output", sessionName+".json")

	if *newSession {
		os.Remove(historyPath)
	}
	hManager := history.NewManager(historyPath)
	if err := hManager.Load(); err != nil {
		log.Fatalf("Error loading history: %v", err)
	}

	registry := tools.NewRegistry()
	tools.RegisterFileSystemTools(registry)
	tools.RegisterSystemTools(registry)
	tools.RegisterStateTools(registry, homeDir, sessionName)

	client, err := api.NewClient(cfg.URL, cfg.Model, authenticator, cfg.ThinkingBudget, cfg.ThinkingLevel, cfg.Person, cfg.UseSearch)
	if err != nil {
		log.Fatalf("Error creating client: %v", err)
	}

	// 6. Execute Agent
	chatAgent := agent.New(client, hManager, registry)
	if err := chatAgent.Chat(prompt); err != nil {
		log.Fatalf("Error: %v", err)
	}

	// 7. Prune and Save
	hManager.Prune(cfg.MaxTurns)
	if err := hManager.Save(); err != nil {
		log.Fatalf("Error saving history: %v", err)
	}
}

