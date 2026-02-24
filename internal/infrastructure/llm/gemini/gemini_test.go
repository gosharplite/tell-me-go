// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

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

func TestSendChat_Scenarios(t *testing.T) {
	tests := []struct {
		name                   string
		mockResponse           genai.GenerateContentResponse
		expectedText           string
		expectedError          string
		systemInstruction      string
		thinkingBudget         int
		thinkingBudgetSeverity string
	}{
		{
			name: "Success",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Role:  "model",
							Parts: []*genai.Part{{Text: "World"}},
						},
					},
				},
			},
			expectedText: "World",
		},
		{
			name: "SafetyBlock",
			mockResponse: genai.GenerateContentResponse{
				PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
					BlockReason: "SAFETY",
				},
			},
			expectedError: "blocked by safety filters",
		},
		{
			name: "FinishReasonSafety",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						FinishReason: genai.FinishReasonSafety,
					},
				},
			},
			expectedError: "Finish Reason: SAFETY",
		},
		{
			name: "SystemInstruction",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
			},
			systemInstruction: "Be helpful and concise",
			expectedText:      "OK",
		},
		{
			name: "ThinkingBudget",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
			},
			thinkingBudget:         1000,
			thinkingBudgetSeverity: "LOW",
			expectedText:           "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify Authorization Header
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("expected bearer token, got '%s'", r.Header.Get("Authorization"))
				}

				// If system instruction is expected, verify it in the request body
				if tt.systemInstruction != "" {
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
					if len(req.SystemInstruction.Parts) == 0 || req.SystemInstruction.Parts[0].Text != tt.systemInstruction {
						t.Errorf("expected system instruction %q, got %v", tt.systemInstruction, req.SystemInstruction.Parts)
					}
				}

				if err := json.NewEncoder(w).Encode(tt.mockResponse); err != nil {
					t.Errorf("failed to encode response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
			authenticator := &auth.VertexAuth{Token: "test-token"}
			client, err := NewClient(
				apiURL,
				"test-model",
				authenticator,
				tt.thinkingBudget,
				tt.thinkingBudgetSeverity,
				0,
				tt.systemInstruction,
				false,
				events.NewSimpleEventBus(),
				5*time.Second,
			)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			history := []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
			}
			content, _, err := client.SendChat(context.Background(), history, nil, nil)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectedError)
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("SendChat failed: %v", err)
			}

			if content.Parts[0].Text != tt.expectedText {
				t.Errorf("expected text %q, got %q", tt.expectedText, content.Parts[0].Text)
			}
		})
	}
}

func TestStreamChat_Scenarios(t *testing.T) {
	tests := []struct {
		name           string
		mockChunks     []genai.GenerateContentResponse
		expectedText   string
		expectedError  string
		expectedTokens int32
	}{
		{
			name: "Success",
			mockChunks: []genai.GenerateContentResponse{
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
			},
			expectedText:   "Hello World!",
			expectedTokens: 2,
		},
		{
			name: "SafetyBlock",
			mockChunks: []genai.GenerateContentResponse{
				{
					PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
						BlockReason: "SAFETY",
					},
				},
			},
			expectedError: "blocked by safety filters",
		},
		{
			name: "FinishReasonSafety",
			mockChunks: []genai.GenerateContentResponse{
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
			},
			expectedError: "stream interrupted (Finish Reason: SAFETY)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				for _, chunk := range tt.mockChunks {
					data, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\r\n\r\n", string(data))
				}
			}))
			t.Cleanup(server.Close)

			apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
			client, _ := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

			var receivedText string
			callback := func(c *llm.Content) {
				for _, p := range c.Parts {
					receivedText += p.Text
				}
			}

			metrics, err := client.StreamChat(context.Background(), []*llm.Content{}, nil, nil, callback)

			if tt.expectedError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectedError)
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("StreamChat failed: %v", err)
			}

			if receivedText != tt.expectedText {
				t.Errorf("expected %q, got %q", tt.expectedText, receivedText)
			}

			if tt.expectedTokens > 0 {
				if metrics == nil || metrics.ResponseTokens != tt.expectedTokens {
					t.Errorf("expected %d response tokens, got %v", tt.expectedTokens, metrics)
				}
			}
		})
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
	t.Cleanup(server.Close)

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	client, _ := NewClient(apiURL, "test-model", authenticator, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	err := client.RefreshAuth()
	if err != nil {
		t.Fatalf("RefreshAuth failed: %v", err)
	}
}
