package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gosharplite/tell-me-go/internal/api"
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
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// 2. Override API Key from Env if present
	if apiKey := os.Getenv("API_KEY"); apiKey != "" {
		cfg.APIKey = apiKey
	}

	if cfg.APIKey == "" {
		log.Fatal("API_KEY not found in config or environment")
	}

	// 3. Initialize API Client
	client := api.NewClient(cfg.URL, cfg.Model, cfg.APIKey)

	// 4. Send Message
	fmt.Printf("> %s\n", prompt)
	response, err := client.SendMessage(prompt)
	if err != nil {
		log.Fatalf("Error from Gemini: %v", err)
	}

	fmt.Printf("\n%s\n", response)
}
