// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/services/summarizer"
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
					UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
						PromptTokenCount:     100,
						CandidatesTokenCount: 50,
						TotalTokenCount:      150,
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
				// Verify metadata propagation
				if m, ok := resp.Metadata["metrics"].(*llm.Metrics); !ok || m == nil {
					t.Errorf("expected metrics in tool result metadata, got: %v", resp.Metadata["metrics"])
				} else if m.PromptTokens != 100 {
					t.Errorf("expected PromptTokens 100 in metrics, got %d", m.PromptTokens)
				}
			}
		})
	}
}

func TestSummarizeRange_SafetyCheck(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "test_safety_history.json")

	mockCounter := &mockTokenCounter{tokens: 950000} // Above 90% of 1M
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
		Summarizer: &mockSummarizer{},
	}

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error due to safety check, got nil")
	}

	expectedPrefix := "summarization failed: the selected 1 turns contain ~950000 tokens, which exceeds the safety limit of 900000. Please try summarizing a smaller number of turns"
	if !strings.HasPrefix(err.Error(), expectedPrefix) {
		t.Errorf("expected error prefix %q, got %q", expectedPrefix, err.Error())
	}
	if !errors.Is(err, llm.ErrContextLimitExceeded) {
		t.Errorf("expected error to wrap llm.ErrContextLimitExceeded")
	}
}

func TestSummarizeRange_Logging(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "test_logging_history.json")
	hManager := history.NewManager(historyFile)
	ctx := context.Background()

	// Add 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "2"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "3"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "4"}}})

	tokenCount := 1234
	mockCounter := &mockTokenCounter{tokens: tokenCount}
	strategy := NewContextStrategy(mockCounter, nil)
	bus := &events.TestEventBus{}

	// Use real summarizer but mock gateway
	mockG := &mockGateway{}
	summarizerImpl := summarizer.NewSummarizer(mockG, bus)

	cm := &ContextManager{
		Strategy:   strategy,
		History:    hManager,
		Summarizer: summarizerImpl,
		Events:     bus,
	}

	turns := 1
	_, _, err := cm.SummarizeRange(ctx, turns, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evs := bus.FilterEvents(reflect.TypeOf(events.SystemMessageEvent{}))
	if len(evs) == 0 {
		t.Fatal("expected SystemMessageEvent to be published")
	}

	found := false
	expectedMsg := fmt.Sprintf("summarize_history: processing %d turns (~%d tokens)", turns, tokenCount)
	for _, e := range evs {
		se := e.(events.SystemMessageEvent)
		if se.Message == expectedMsg {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected log message %q not found in published events", expectedMsg)
	}

	// Verify that the OLD log message is NOT present
	oldMsgPrefix := "Summarizing"
	for _, e := range evs {
		se := e.(events.SystemMessageEvent)
		if len(se.Message) >= len(oldMsgPrefix) && se.Message[:len(oldMsgPrefix)] == oldMsgPrefix {
			t.Errorf("found old log message %q which should have been removed", se.Message)
		}
	}
}
