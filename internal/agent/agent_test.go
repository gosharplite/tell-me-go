// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"google.golang.org/genai"
)

func TestAgentToolLoop(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(historyFile)
	registry := tools.NewRegistry()

	// Register a dummy tool
	registry.Register(&genai.FunctionDeclaration{
		Name:       "get_weather",
		Parameters: &genai.Schema{Type: genai.TypeObject},
	}, func(args map[string]interface{}) (string, error) {
		return "Sunny", nil
	})

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var apiResp genai.GenerateContentResponse

		if callCount == 1 {
			// First call returns a thought and a function call
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Role: "model",
							Parts: []*genai.Part{
								{Text: "I should check the weather.", Thought: true, ThoughtSignature: []byte("sig123")},
								{FunctionCall: &genai.FunctionCall{Name: "get_weather", Args: map[string]interface{}{}}},
							},
						},
					},
				},
			}
		} else {
			// Second call returns final text
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Role:  "model",
							Parts: []*genai.Part{{Text: "It is sunny."}},
						},
					},
				},
			}
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	// Setup client with mock server
	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	a := New(client, hManager, registry)

	// Execute Chat
	err = a.Chat("What's the weather?")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify history sequence and signatures
	contents := hManager.GetContents()
	if len(contents) != 4 {
		t.Fatalf("Expected 4 history entries, got %d", len(contents))
	}

	// Entry 1: User
	// Entry 2: Model (Thought + FunctionCall)
	if string(contents[1].Parts[0].ThoughtSignature) != "sig123" {
		t.Errorf("Thought signature lost in history")
	}
	// Entry 3: Function Response
	if contents[2].Role != genai.RoleUser {
		t.Errorf("Expected role 'user' for tool result, got %s", contents[2].Role)
	}
	// Entry 4: Final Model Response
}
