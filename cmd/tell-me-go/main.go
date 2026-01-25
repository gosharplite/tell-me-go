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
	"github.com/gosharplite/tell-me-go/internal/tools"
)

func main() {
	// 1. Define Flags
	configPath := flag.String("c", "configs/gemini.yaml", "Path to the configuration file")
	newSession := flag.Bool("new", false, "Start a new session")
	flag.Parse()

	// 2. Handle Prompt Argument
	prompt := flag.Arg(0)
	if prompt == "" {
		// If no argument, check if stdin is a pipe or wait for user input (multi-line)
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Piped input
			var b []byte
			b, err := os.ReadFile(os.Stdin.Name())
			if err == nil {
				prompt = string(b)
			}
		} else {
			// Interactive multi-line input
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
		authenticator = &auth.VertexAuth{}
	} else if cfg.APIKey != "" {
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

	// 7. Initialize API Client and Tools
	registry := tools.NewRegistry()
	tools.RegisterFileSystemTools(registry)
	client := api.NewClient(cfg.URL, cfg.Model, authenticator)

	// 8. Add Prompt to History
	if err := hManager.AddEntry("user", prompt); err != nil {
		log.Fatalf("Error adding entry to history: %v", err)
	}

	// 9. Main Interaction Loop
	fmt.Fprintf(os.Stderr, "\033[0;32m> %s\033[0m\n", prompt)

	for {
		content, err := client.SendChat(hManager.GetContents(), registry.ToToolJSON())
		if err != nil {
			log.Fatalf("Error from Gemini: %v", err)
		}

		// Add Model Response to History
		if err := hManager.AddContent(*content); err != nil {
			log.Fatalf("Error adding model response to history: %v", err)
		}

		// Process Parts
		hasFunctionCall := false
		var toolParts []api.Part

		for _, part := range content.Parts {
			if part.Thought {
				fmt.Fprintf(os.Stderr, "\033[0;90m[Thinking] %s\033[0m\n", part.Text)
				continue
			}

			if part.FunctionCall != nil {
				hasFunctionCall = true
				fmt.Fprintf(os.Stderr, "\033[0;90m[Tool] Calling: %s(%v)\033[0m\n", part.FunctionCall.Name, part.FunctionCall.Args)

				result, err := registry.Execute(part.FunctionCall.Name, part.FunctionCall.Args)
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
				}

				toolParts = append(toolParts, api.Part{
					FunctionResponse: &api.FunctionResponse{
						Name: part.FunctionCall.Name,
						Response: map[string]interface{}{
							"result": result,
						},
					},
				})
			}

			if part.Text != "" && !part.Thought {
				fmt.Printf("\n%s\n", part.Text)
			}
		}

		if !hasFunctionCall {
			break
		}

		// Add Tool Responses to History and Continue Loop
		if err := hManager.AddContent(api.Content{
			Role:  "function",
			Parts: toolParts,
		}); err != nil {
			log.Fatalf("Error adding tool response to history: %v", err)
		}
	}

	// 10. Prune and Save
	hManager.Prune(cfg.MaxTurns)
	if err := hManager.Save(); err != nil {
		log.Fatalf("Error saving history: %v", err)
	}
}
