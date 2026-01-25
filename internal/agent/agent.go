// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agent coordinates the interaction loop between the user, the AI model, and local tools.
package agent

import (
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"google.golang.org/genai"
)

// Agent handles the orchestration of the chat loop.
type Agent struct {
	Client   *api.Client
	History  *history.Manager
	Registry *tools.Registry
}

// New creates a new Agent instance.
func New(client *api.Client, hManager *history.Manager, registry *tools.Registry) *Agent {
	return &Agent{
		Client:   client,
		History:  hManager,
		Registry: registry,
	}
}

// Chat handles a single user prompt and processes any subsequent tool calls.
func (a *Agent) Chat(prompt string) error {
	if err := a.History.AddEntry(genai.RoleUser, prompt); err != nil {
		return fmt.Errorf("failed to add user prompt: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\033[0;32m> %s\033[0m\n", prompt)

	for {
		content, err := a.Client.SendChat(a.History.GetContents(), a.Registry.ToToolSDK())
		if err != nil {
			return err
		}

		// Add Model Response to History (including thoughts and signatures)
		if err := a.History.AddContent(content); err != nil {
			return fmt.Errorf("history violation: %w", err)
		}

		hasFunctionCall := false
		var toolParts []*api.Part

		for _, part := range content.Parts {
			if part.Thought {
				fmt.Fprintf(os.Stderr, "\033[0;90m[Thinking] %s\033[0m\n", part.Text)
				continue
			}

			if part.FunctionCall != nil {
				hasFunctionCall = true
				fmt.Fprintf(os.Stderr, "\033[0;90m[Tool] Calling: %s(%v)\033[0m\n", part.FunctionCall.Name, part.FunctionCall.Args)

				result, err := a.Registry.Execute(part.FunctionCall.Name, part.FunctionCall.Args)
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
				}

				toolParts = append(toolParts, &api.Part{
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
		// In the new GenAI SDK, function responses are sent with the 'user' role.
		if err := a.History.AddContent(&api.Content{
			Role:  genai.RoleUser,
			Parts: toolParts,
		}); err != nil {
			return fmt.Errorf("failed to add tool response: %w", err)
		}
	}

	return nil
}
