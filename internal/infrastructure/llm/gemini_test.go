// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
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
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// 2. Setup client with mock server URL and mock authenticator
	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	client, err := NewGeminiClient(apiURL, "test-model", authenticator, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 3. Execution
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
	}
	content, _, err := client.SendChat(context.Background(), history, nil, nil)
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
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	_, _, err := client.SendChat(context.Background(), []*llm.Content{}, nil, nil)
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
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	_, _, err := client.SendChat(context.Background(), []*llm.Content{}, nil, nil)
	if err == nil {
		t.Fatalf("expected error for finish reason SAFETY, got nil")
	}
	if !strings.Contains(err.Error(), "Finish Reason: SAFETY") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSystemInstruction(t *testing.T) {
	expectedInstruction := "Be helpful and concise"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify payload
		var req struct {
			SystemInstruction struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"systemInstruction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if len(req.SystemInstruction.Parts) == 0 {
			t.Error("expected system instruction parts, got none")
		} else if req.SystemInstruction.Parts[0].Text != expectedInstruction {
			t.Errorf("expected instruction %q, got %q", expectedInstruction, req.SystemInstruction.Parts[0].Text)
		}

		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, expectedInstruction, false, events.NewSimpleEventBus(), 5*time.Second)

	_, _, err := client.SendChat(context.Background(), []*llm.Content{}, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func TestThinkingBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 1000, "LOW", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	_, _, err := client.SendChat(context.Background(), []*llm.Content{}, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func TestStreamChat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		chunks := []genai.GenerateContentResponse{
			{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: "Hello "}},
						},
					},
				},
			},
			{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: "World!"}},
						},
						FinishReason: genai.FinishReasonStop,
					},
				},
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					CandidatesTokenCount: 2,
					PromptTokenCount:     1,
					TotalTokenCount:      3,
				},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\r\n\r\n", string(data))
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	var receivedText string
	callback := func(c *llm.Content) {
		for _, p := range c.Parts {
			receivedText += p.Text
		}
	}

	metrics, err := client.StreamChat(context.Background(), []*llm.Content{}, nil, nil, callback)
	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}

	if receivedText != "Hello World!" {
		t.Errorf("expected 'Hello World!', got '%s'", receivedText)
	}

	if metrics == nil || metrics.ResponseTokens != 2 {
		t.Errorf("expected 2 response tokens, got %v", metrics)
	}
}

func TestStreamChat_SafetyBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		chunks := []genai.GenerateContentResponse{
			{
				PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
					BlockReason: "SAFETY",
				},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\r\n\r\n", string(data))
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	_, err := client.StreamChat(context.Background(), []*llm.Content{}, nil, nil, func(c *llm.Content) {})
	if err == nil {
		t.Fatal("expected error for safety block, got nil")
	}
	if !strings.Contains(err.Error(), "blocked by safety filters") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStreamChat_FinishReason_Safety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		chunks := []genai.GenerateContentResponse{
			{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Parts: []*genai.Part{{Text: "Some text"}},
						},
						FinishReason: genai.FinishReasonSafety,
					},
				},
			},
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\r\n\r\n", string(data))
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewGeminiClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	_, err := client.StreamChat(context.Background(), []*llm.Content{}, nil, nil, func(c *llm.Content) {})
	if err == nil {
		t.Fatal("expected error for finish reason safety, got nil")
	}
	if !strings.Contains(err.Error(), "stream interrupted (Finish Reason: SAFETY)") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRefreshAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	client, _ := NewGeminiClient(apiURL, "test-model", authenticator, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	err := client.RefreshAuth()
	if err != nil {
		t.Fatalf("RefreshAuth failed: %v", err)
	}
}
