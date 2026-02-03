// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"google.golang.org/genai"
)

func TestAgent_SummarizeHistory(t *testing.T) {
	tests := []struct {
		name           string
		turns          float64
		historyTurns   int
		expectedMsgs   int
		expectedErr    bool
		expectedResult string
	}{
		{
			name:         "summarize some turns",
			turns:        3,
			historyTurns: 5,
			expectedMsgs: 6, // 10 - 6 + 2 = 6
		},
		{
			name:         "invalid turns zero",
			turns:        0,
			historyTurns: 5,
			expectedErr:  true,
		},
		{
			name:         "invalid turns negative",
			turns:        -5,
			historyTurns: 5,
			expectedErr:  true,
		},
		{
			name:           "clamp too many turns",
			turns:          100,
			historyTurns:   2, // 4 messages
			expectedMsgs:   4, // (4-2)/2 = 1 turn. 4 - 2 + 2 = 4.
			expectedResult: "Summarized the first 1 turns of history.",
		},
		{
			name:           "history too short",
			turns:          2,
			historyTurns:   1, // 2 messages
			expectedResult: "History is too short to summarize yet.",
		},
		{
			name:         "with focus",
			turns:        1,
			historyTurns: 2,
			expectedMsgs: 4, // 4 - 2 + 2 = 4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
			reg := registry.New()
			ctx := context.Background()

			// Fill history with some turns
			for i := 1; i <= tt.historyTurns; i++ {
				_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Turn User"}}})
				_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Turn Model"}}})
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiResp := genai.GenerateContentResponse{
					Candidates: []*genai.Candidate{
						{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "This is a summary."}}}},
					},
				}
				json.NewEncoder(w).Encode(apiResp)
			}))
			defer server.Close()

			apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
			client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			sm := security.NewSecurityManager(nil)
			a := New(client, hManager, reg, sm, true)

			args := map[string]interface{}{
				"turns": tt.turns,
			}
			if tt.name == "with focus" {
				args["focus"] = "refactoring"
			}
			it := NewInternalTools(a.ctxManager)
			resp, err := it.SummarizeHistory(ctx, args)

			if (err != nil) != tt.expectedErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectedErr, err)
			}

			if tt.expectedErr {
				return
			}

			if tt.expectedResult != "" && resp.Text != tt.expectedResult {
				t.Errorf("expected result text %q, got %q", tt.expectedResult, resp.Text)
			}

			if tt.expectedMsgs > 0 {
				contents := hManager.GetContents()
				if len(contents) != tt.expectedMsgs {
					t.Errorf("expected %d messages in history, got %d", tt.expectedMsgs, len(contents))
				}
			}
		})
	}
}

func TestSummarizeRange_SafetyCheck(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "test_safety_history.json")

	mockCounter := &mockTokenCounter{count: 950000} // Above 90% of 1M
	strategy := NewContextStrategy(mockCounter, nil)
	hManager := history.NewManager(historyFile)
	
	ctx := context.Background()
	// Add 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "2"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "3"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "4"}}})

	cm := &ContextManager{
		Strategy:   strategy,
		History:    hManager,
		Summarizer: &mockSafetySummarizer{},
	}

	_, err := cm.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error due to safety check, got nil")
	}

	expectedErr := "summarization failed: the selected 1 turns contain ~950000 tokens, which exceeds the safety limit of 900000. Please try summarizing a smaller number of turns"
	if err.Error() != expectedErr {
		t.Errorf("expected error %q, got %q", expectedErr, err.Error())
	}
}

type mockTokenCounter struct {
	count int
}

func (m *mockTokenCounter) Count(contents []*llm.Content) int {
	return m.count
}

type mockSafetySummarizer struct{}

func (m *mockSafetySummarizer) Summarize(ctx context.Context, contents []*llm.Content, focus string) (string, error) {
	return "summary", nil
}
