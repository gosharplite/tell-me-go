// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

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
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/gemini"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"google.golang.org/genai"
)

type summarizeTestCase struct {
	name           string
	turns          float64
	historyTurns   int
	expectedMsgs   int
	expectedErr    bool
	expectedResult string
}

func TestAgent_SummarizeHistory_Success(t *testing.T) {
	tests := []summarizeTestCase{
		{
			name:         "summarize some turns",
			turns:        3,
			historyTurns: 5,
			expectedMsgs: 6, // 10 - 6 + 2 = 6
		},
		{
			name:           "clamp too many turns",
			turns:          100,
			historyTurns:   2, // 4 messages
			expectedMsgs:   4, // (4-2)/2 = 1 turn. 4 - 2 + 2 = 4.
			expectedResult: "summarized the first 1 turns of history",
		},
		{
			name:           "history too short",
			turns:          2,
			historyTurns:   1, // 2 messages
			expectedResult: "history is too short to summarize yet",
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
			runSummarizeTest(t, tt)
		})
	}
}

func TestAgent_SummarizeHistory_Errors(t *testing.T) {
	tests := []summarizeTestCase{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSummarizeTest(t, tt)
		})
	}
}

func runSummarizeTest(t *testing.T, tt summarizeTestCase) {
	ctx := context.Background()
	hManager := setupTestHistory(t, tt.historyTurns)

	server := setupMockGeminiServer()
	defer server.Close()

	client := setupTestClient(t, server.URL)
	it := setupInternalTools(t, client, hManager)

	args := map[string]interface{}{"turns": tt.turns}
	if tt.name == "with focus" {
		args["focus"] = "refactoring"
	}

	resp, err := it.SummarizeHistory(ctx, args, nil)
	verifySummarizeResult(t, tt, resp, err, hManager)
}

func setupTestHistory(t *testing.T, turns int) ports.HistoryManager {
	t.Helper()
	historyPath := filepath.Join(t.TempDir(), "history.json")
	h := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()
	for i := 1; i <= turns; i++ {
		_ = h.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "Turn User"}}})
		_ = h.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "Turn Model"}}})
	}
	return h
}

func setupMockGeminiServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		_ = json.NewEncoder(w).Encode(apiResp)
	}))
}

func setupTestClient(t *testing.T, url string) *gemini.Client {
	t.Helper()
	apiURL := url + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	client, err := gemini.NewClient(apiURL, "test-model", &auth.BearerAuth{Token: "test"}, gemini.WithEventBus(bus), gemini.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

func setupInternalTools(t *testing.T, client *gemini.Client, h ports.HistoryManager) *session.InternalTools {
	t.Helper()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	reg := registry.New()
	gw := llm.NewResilientClient(client)
	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(reg))
	factory := &sessctx.Factory{
		Registry:   reg,
		History:    h,
		Summarizer: llm.NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}
	cm := sessctx.NewManager(strategy, h, bus, factory)
	return session.NewInternalTools(cm, &ports.NoOpLogger{})
}

func verifySummarizeResult(t *testing.T, tt summarizeTestCase, resp tools.ToolResult, err error, h ports.HistoryManager) {
	t.Helper()
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
		contents, _ := h.GetWindow(context.Background(), 0, -1)
		if len(contents) != tt.expectedMsgs {
			t.Errorf("expected %d messages in history, got %d", tt.expectedMsgs, len(contents))
		}
		// Verify metadata propagation
		if m, ok := resp.Metadata["metrics"].(*domain_llm.Metrics); !ok || m == nil {
			t.Errorf("expected metrics in tool result metadata, got: %v", resp.Metadata["metrics"])
		} else if m.PromptTokens != 100 {
			t.Errorf("expected PromptTokens 100 in metrics, got %d", m.PromptTokens)
		}
	}
}

func TestSummarizeRange_SafetyCheck(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "test_safety_history.json")

	mockCounter := &agenttest.MockTokenCounter{}
	mockCounter.SetTokens(950000) // Above 90% of 1M
	strategy := sessctx.NewStrategy(mockCounter)
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyFile, historyFile+".archive")

	ctx := context.Background()
	// Add 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "1"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "2"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "3"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "4"}}})

	cm := sessctx.NewManager(strategy, hManager, nil, nil)
	cm.Summarizer = &agenttest.MockSummarizer{}

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Fatal("expected error due to safety check, got nil")
	}

	expectedPart := "summarization failed: the selected 1 turns contain ~950000 tokens, which exceeds the safety limit of 900000. Please try summarizing a smaller number of turns"
	if !strings.Contains(err.Error(), expectedPart) {
		t.Errorf("expected error containing %q, got %q", expectedPart, err.Error())
	}
	if !errors.Is(err, domain_llm.ErrContextLimitExceeded) {
		t.Errorf("expected error to wrap domain_llm.ErrContextLimitExceeded")
	}
}

func TestSummarizeRange_Logging(t *testing.T) {
	historyFile := filepath.Join(t.TempDir(), "test_logging_history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyFile, historyFile+".archive")
	ctx := context.Background()

	// Add 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "1"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "2"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "3"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "4"}}})

	tokenCount := 1234
	mockCounter := &agenttest.MockTokenCounter{}
	mockCounter.SetTokens(tokenCount)
	strategy := sessctx.NewStrategy(mockCounter)
	bus := &eventstest.TestEventBus{}

	// Use real summarizer but mock gateway
	mockG := &agenttest.MockGateway{}
	mockG.GenerateFunc = func(ctx context.Context, input []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
		return &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "summary"}}}, &domain_llm.Metrics{}, nil
	}
	summarizerImpl := llm.NewSummarizer(mockG, bus)

	cm := sessctx.NewManager(strategy, hManager, bus, nil)
	cm.Summarizer = summarizerImpl

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
}
