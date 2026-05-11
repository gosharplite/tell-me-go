// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestContextManager_AutoSummarizeTrigger(t *testing.T) {
	hManager, cm, _, server := setupAutoSummarizeTest(t)
	defer server.Close()

	// Note: Unlike the original inline version, we do not register a "dummy_tool".
	// setupAutoSummarizeTest leaves the registry empty, which is intentional —
	// this test only cares that summarization triggers, and the 95k-token payload
	// (~5× above the 90k threshold) dwarfs any tool-overhead variance (~200 tokens).

	// Set tight limit to trigger summarization.
	// Use 100000. Safety limit = 99000. 90% = 90000.
	if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 100000, MaxToolTurns: 10, MaxHistoryTurns: 20}); err != nil {
		t.Fatalf("Reconfigure setup failed: %v", err)
	}

	// Add 95k tokens of history (9 turns = 18 messages).
	addHeavyHistory(t, hManager, 9)
	assertHistoryLength(t, hManager, 18)

	// Call Prepare, which should trigger AutoSummarize on the context window.
	preparedHistory, metadata, err := cm.Prepare(context.Background(), 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	t.Logf("Tokens after Prepare: %d", metadata.FinalTokenCount)

	// 18 messages → 4 turns summarized (8 msgs) + 2 summary msgs = 12.
	assertPreparedLength(t, preparedHistory, 12)
	assertFirstMessageContains(t, preparedHistory, "system auto-summary")
	assertHistoryLength(t, hManager, 12)
}

func TestAutoSummarize_Logging(t *testing.T) {
	ctx := context.Background()
	hManager, cm, bus, server := setupAutoSummarizeTest(t)
	defer server.Close()

	// Set a limit to trigger auto-summarization (90% threshold = 90k tokens)
	if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 100000, MaxToolTurns: 10, MaxHistoryTurns: 20}); err != nil {
		t.Fatalf("Reconfigure setup failed: %v", err)
	}

	// Add enough turns to exceed 90k tokens
	addHeavyHistory(t, hManager, 9)

	// Channel to capture the log event
	logReceived := make(chan string, 1)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if msg, ok := e.(events.SystemMessageEvent); ok {
			if strings.Contains(strings.ToLower(msg.Message), "auto-summarizing") {
				logReceived <- msg.Message
			}
		}
	})

	// Trigger the pipeline via Prepare
	_, _, err := cm.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify the log was emitted and contains expected data
	verifyAutoSummarizeLog(t, logReceived)
}

func setupAutoSummarizeTest(t *testing.T) (ports.HistoryManager, *sessctx.Manager, events.EventBus, *httptest.Server) {
	t.Helper()
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "log_test_history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	reg := registry.New()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Summary"}}}},
			},
		}
		_ = json.NewEncoder(w).Encode(apiResp)
	}))

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, _ := gemini.NewClient(apiURL, "test", &auth.BearerAuth{Token: "t"}, gemini.WithEventBus(bus), gemini.WithTimeout(5*time.Second))

	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(reg))
	gw := llm.NewResilientClient(client)
	factory := &sessctx.Factory{
		Registry:   reg,
		History:    hManager,
		Summarizer: llm.NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}
	cm := sessctx.NewManager(strategy, hManager, bus, factory)

	return hManager, cm, bus, server
}

func addHeavyHistory(t *testing.T, h ports.HistoryManager, turns int) {
	t.Helper()
	ctx := context.Background()
	longText := strings.Repeat("A", 32000) // approx 10k tokens
	for i := 0; i < turns; i++ {
		_ = h.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: longText}}})
		_ = h.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "Response"}}})
	}
}

func assertHistoryLength(t *testing.T, h ports.HistoryManager, expected int) {
	t.Helper()
	contents, err := h.GetWindow(context.Background(), 0, -1)
	if err != nil {
		t.Fatalf("GetWindow failed: %v", err)
	}
	if len(contents) != expected {
		t.Errorf("expected %d messages in persistent store, got %d", expected, len(contents))
	}
}

func assertPreparedLength(t *testing.T, prepared []*domain_llm.Content, expected int) {
	t.Helper()
	if len(prepared) != expected {
		t.Errorf("expected %d messages in prepared history, got %d", expected, len(prepared))
	}
}

func assertFirstMessageContains(t *testing.T, prepared []*domain_llm.Content, substr string) {
	t.Helper()
	if len(prepared) == 0 {
		t.Fatal("prepared history is empty, cannot check first message")
	}
	if !strings.Contains(prepared[0].Parts[0].Text, substr) {
		t.Errorf("first message should contain %q, got: %s", substr, prepared[0].Parts[0].Text)
	}
}

func verifyAutoSummarizeLog(t *testing.T, logCh <-chan string) {
	t.Helper()
	select {
	case msg := <-logCh:
		t.Logf("Caught expected event: %s", msg)
		if !strings.Contains(msg, "turns") || !strings.Contains(msg, "tokens") {
			t.Errorf("Log message format incorrect, got: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout: Auto-summarization log event was never emitted")
	}
}

func TestContextManager_AutoSummarizeWithSystemInstructions(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history_sys.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	reg := registry.New()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Summary"}}}},
			},
		}
		if err := json.NewEncoder(w).Encode(apiResp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	// Set initial system instructions
	client, _ := gemini.NewClient(apiURL, "test", &auth.BearerAuth{Token: "t"}, gemini.WithSystemInstruction("Initial System Instruction"), gemini.WithEventBus(bus), gemini.WithTimeout(5*time.Second))

	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(reg))
	gw := llm.NewResilientClient(client)
	factory := &sessctx.Factory{
		Registry:   reg,
		History:    hManager,
		Summarizer: llm.NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}
	cm := sessctx.NewManager(strategy, hManager, bus, factory)
	if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 3500, MaxToolTurns: 10, MaxHistoryTurns: 20}); err != nil {
		t.Fatalf("Reconfigure setup failed: %v", err)
	} // Limit to trigger summarization

	// Add some turns (approx 3451 tokens with base overhead and tools)
	longText := strings.Repeat("A", 1600) // approx 500 tokens
	for i := 0; i < 6; i++ {
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: longText}}})
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "Response"}}})
	}

	// Trigger Prepare
	preparedHistory, _, err := cm.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Should have summarized 6/2 = 3 turns (6 messages) replaced by 2 messages.
	// Total: 12 - 6 + 2 = 8 messages.
	if len(preparedHistory) != 8 {
		t.Errorf("expected 8 messages in context window, got %d", len(preparedHistory))
	}

	// First message should be the auto-summary
	if !strings.Contains(preparedHistory[0].Parts[0].Text, "system auto-summary") {
		t.Errorf("first message should be auto-summary, got: %s", preparedHistory[0].Parts[0].Text)
	}

	// Ensure no "system" role messages in history (system instructions are client-side)
	for _, c := range preparedHistory {
		if c.Role == "system" {
			t.Errorf("found system role message in context window, which should be avoided")
		}
	}

	// Verify persistent store was updated
	historyContents, _ := hManager.GetWindow(ctx, 0, -1)
	if len(historyContents) != 8 {
		t.Errorf("expected 8 messages in persistent store after auto-summarization, got %d", len(historyContents))
	}
}

func TestToolInjectedTokenBudgetPressure(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "tool_pressure_history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	reg := registry.New()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	// 1. Register many tools to create a large schema (approx 2000 tokens)
	for i := 0; i < 20; i++ {
		if err := reg.Register(&tools.ToolDeclaration{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: "A tool with a very long description to consume more tokens in the schema " + strings.Repeat("detail ", 20),
			Parameters:  &tools.Schema{Type: "OBJECT"},
		}, nil); err != nil {
			t.Fatalf("failed to register tool_%d: %v", i, err)
		}
	}

	// 2. Mock server for summarization
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Compressed history summary"}}}},
			},
		}
		if err := json.NewEncoder(w).Encode(apiResp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, _ := gemini.NewClient(apiURL, "test", &auth.BearerAuth{Token: "t"}, gemini.WithEventBus(bus), gemini.WithTimeout(5*time.Second))

	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(reg))
	gw := llm.NewResilientClient(client)
	factory := &sessctx.Factory{
		Registry:   reg,
		History:    hManager,
		Summarizer: llm.NewSummarizer(gw, bus),
		Estimator:  strategy,
		Events:     bus,
	}
	cm := sessctx.NewManager(strategy, hManager, bus, factory)

	// 3. Set a tight token limit.
	// Base overhead ~300.
	// Tools ~2000.
	// Total overhead ~2300.
	// Set limit to 3000.
	if err := cm.Reconfigure(events.Limits{MaxHistoryTokens: 3000, MaxToolTurns: 10, MaxHistoryTurns: 20}); err != nil {
		t.Fatalf("Reconfigure setup failed: %v", err)
	}

	// 4. Add history (600 tokens)
	// Total = 2300 (overhead) + 600 (history) = 2900.
	// 90% of 3000 = 2700.
	// 2900 > 2700, so it should trigger summarization.
	ctx := context.Background()
	longText := strings.Repeat("B", 1920) // approx 600 tokens
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: longText}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "Short response"}}})

	// Add more turns to have something to summarize (need at least 10 messages for auto-summarize)
	for i := 0; i < 4; i++ {
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "Turn message"}}})
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "ok"}}})
	}
	// Total messages = 2 + 8 = 10.

	// 5. Call Prepare. It should trigger auto-summarize because of tool schema injection.
	preparedHistory, metadata, err := cm.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	if !metadata.SummarizationAttempted {
		t.Errorf("Expected auto-summarization to be attempted due to tool schema token pressure, but it wasn't.")
	}

	// Verify that the resulting context window is shorter
	if len(preparedHistory) >= 10 {
		t.Errorf("Expected context window to be pruned/summarized, but still have %d messages", len(preparedHistory))
	}

	// Verify that the persistent store was updated
	historyContents, _ := hManager.GetWindow(ctx, 0, -1)
	if len(historyContents) != 8 {
		t.Errorf("Expected persistent store to be updated after auto-summarization, got %d messages", len(historyContents))
	}
}
