// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: tell-me-go <prompt>")
		os.Exit(1)
	}
	prompt := os.Args[1]

	// 1. Load Config
	cfgPath := "configs/gemini.yaml"
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		// Fallback to local if configs/ doesn't exist (e.g. running from root)
		cfgPath = "configs/gemini.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// 2. Override API Key from Env if present
	if apiKey := os.Getenv("API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}

	// 3. Determine Authentication Method
	var authenticator auth.Authenticator
	if strings.Contains(cfg.URL, "aiplatform.googleapis.com") || cfg.APIKey == "" {
		// Vertex AI / GCP Token mode
		fmt.Println("[System] Using Vertex AI / GCP Token Authentication")
		authenticator = &auth.VertexAuth{}
	} else {
		// AI Studio / API Key mode
		fmt.Println("[System] Using AI Studio / API Key Authentication")
		authenticator = &auth.APIKeyAuth{APIKey: cfg.APIKey}
	}

	// 4. Initialize API Client
	client := api.NewClient(cfg.URL, cfg.Model, authenticator)

	// 5. Send Message
	fmt.Printf("> %s\n", prompt)
	response, err := client.SendMessage(prompt)
	if err != nil {
		log.Fatalf("Error from Gemini: %v", err)
	}

	fmt.Printf("\n%s\n", response)
}
