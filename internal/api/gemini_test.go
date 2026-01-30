// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/auth"
	"google.golang.org/genai"
)

func TestSendChat(t *testing.T) {
	// 1. Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Headers
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected bearer token, got '%s'", r.Header.Get("Authorization"))
		}

		// Send mock response in SDK-compatible format
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "World"}},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 2. Setup client with mock server URL and mock authenticator
	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	client, err := NewClient(apiURL, "test-model", authenticator, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 3. Execution
	history := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "Hello"}}},
	}
	content, _, err := client.SendChat(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	// 4. Verification
	if content.Parts[0].Text != "World" {
		t.Errorf("expected 'World', got '%s'", content.Parts[0].Text)
	}
}

func TestSendChat_SafetyBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
				BlockReason: "SAFETY",
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)

	_, _, err := client.SendChat(context.Background(), []*genai.Content{}, nil)
	if err == nil {
		t.Fatalf("expected error for safety block, got nil")
	}
	if !strings.Contains(err.Error(), "blocked by safety filters") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSendChat_FinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					FinishReason: genai.FinishReasonSafety,
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)

	_, _, err := client.SendChat(context.Background(), []*genai.Content{}, nil)
	if err == nil {
		t.Fatalf("expected error for finish reason SAFETY, got nil")
	}
	if !strings.Contains(err.Error(), "Finish Reason: SAFETY") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSystemInstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "Be helpful", false)

	_, _, err := client.SendChat(context.Background(), []*genai.Content{}, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func TestThinkingBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 1000, "LOW", nil, "", false)

	_, _, err := client.SendChat(context.Background(), []*genai.Content{}, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}
