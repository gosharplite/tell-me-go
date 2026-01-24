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

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/history"
)

func main() {
	// 1. Define Flags
	configPath := flag.String("c", "configs/gemini.yaml", "Path to the configuration file")
	newSession := flag.Bool("new", false, "Start a new session")
	flag.Parse()

	// 2. Handle Prompt Argument
	prompt := flag.Arg(0)
	if prompt == "" {
		fmt.Println("Usage: tell-me-go [flags] <prompt>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// 3. Load Config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Error loading config [%s]: %v", *configPath, err)
	}

	// 4. Override API Key from Env if present
	if apiKey := os.Getenv("API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}

	// 5. Determine Authentication Method
	var authenticator auth.Authenticator
	isVertex := strings.Contains(cfg.URL, "aiplatform.googleapis.com")

	if isVertex {
		fmt.Println("[System] Using Vertex AI / GCP Token Authentication")
		authenticator = &auth.VertexAuth{}
	} else if cfg.APIKey != "" {
		fmt.Println("[System] Using AI Studio / API Key Authentication")
		authenticator = &auth.APIKeyAuth{APIKey: cfg.APIKey}
	} else {
		log.Fatal("API_KEY not found in config or environment for AI Studio endpoint.")
	}

	// 6. Initialize History Manager
	historyPath := filepath.Join("output", fmt.Sprintf("last-%s.json", cfg.Mode))
	if *newSession {
		os.Remove(historyPath)
	}
	hManager := history.NewManager(historyPath)
	if err := hManager.Load(); err != nil {
		log.Fatalf("Error loading history: %v", err)
	}

	// 7. Initialize API Client
	client := api.NewClient(cfg.URL, cfg.Model, authenticator)

	// 8. Add Prompt to History
	if err := hManager.AddEntry("user", prompt); err != nil {
		log.Fatalf("Error adding entry to history: %v", err)
	}

	// 9. Send Chat
	fmt.Printf("> %s\n", prompt)
	response, err := client.SendChat(hManager.GetContents())
	if err != nil {
		log.Fatalf("Error from Gemini: %v", err)
	}

	// 10. Add Model Response to History and Save
	if err := hManager.AddEntry("model", response); err != nil {
		log.Fatalf("Error adding model response to history: %v", err)
	}

	// 11. Prune and Save
	hManager.Prune(cfg.MaxTurns)
	if err := hManager.Save(); err != nil {
		log.Fatalf("Error saving history: %v", err)
	}

	fmt.Printf("\n%s\n", response)
}
