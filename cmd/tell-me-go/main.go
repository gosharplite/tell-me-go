// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/config"
)

func main() {
	// 1. Define Flags
	configPath := flag.String("c", "configs/gemini.yaml", "Path to the configuration file")
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
		// No key for AI Studio URL
		log.Fatal("API_KEY not found in config or environment for AI Studio endpoint. \nIf you intended to use Vertex AI, ensure your AIURL is correct in the config file.")
	}

	// 6. Initialize API Client
	client := api.NewClient(cfg.URL, cfg.Model, authenticator)

	// 7. Send Message
	fmt.Printf("> %s\n", prompt)
	response, err := client.SendMessage(prompt)
	if err != nil {
		log.Fatalf("Error from Gemini: %v", err)
	}

	fmt.Printf("\n%s\n", response)
}
