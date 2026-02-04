// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

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

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"google.golang.org/genai"
)

func TestContextManager_AutoSummarizeTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{
		Name:        "dummy_tool",
		Description: "A dummy tool for token estimation stability",
	}, nil)
	ctx := context.Background()

	// Mock server for summarization
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Auto-summary content"}}}},
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
	// Disable streaming for testing convenience with httptest
	a := New(client, hManager, reg, sm, true)

	// Set a token limit to trigger auto-summarization.
	// Use 100000. Safety limit = 99000. 90% = 90000.
	a.SetLimits(10, 100000, 20)

	// Add 95k tokens of history.
	longText := strings.Repeat("A", 32000) // approx 10k tokens
	for i := 0; i < 9; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: longText}}})
		_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response"}}})
	}

	// Verify initial count
	initialContents := hManager.GetContents()
	if len(initialContents) != 18 {
		t.Fatalf("expected 18 messages, got %d", len(initialContents))
	}

	// Call Prepare, which should trigger AutoSummarize
	_, metadata, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	t.Logf("Tokens after Prepare: %d", metadata.FinalTokenCount)

	// Check if history was replaced
	newContents := hManager.GetContents()
	// Initial 18 messages (9 turns).
	// maxTurnsToSummarize = 9 / 2 = 4.
	// 4 turns (8 messages) replaced by 2.
	// Total: 18 - 8 + 2 = 12 messages.
	if len(newContents) != 12 {
		t.Errorf("expected 12 messages after auto-summarization, got %d", len(newContents))
	}

	// Index 0 should be the auto-summary user message
	if !strings.Contains(newContents[0].Parts[0].Text, "System Auto-Summary") {
		t.Errorf("first message should be auto-summary, got: %s", newContents[0].Parts[0].Text)
	}
}

func TestAutoSummarize_Logging(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "log_test_history.json"))
	reg := registry.New()
	ctx := context.Background()

	// Mock server that returns a simple summary
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Summary"}}}},
			},
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test", &auth.VertexAuth{Token: "t"}, 0, "", 0, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	a := New(client, hManager, reg, security.NewSecurityManager(nil), true)

	// Set a limit to trigger auto-summarization (90% threshold = 90k tokens)
	a.SetLimits(10, 100000, 20)

	// Add enough turns to exceed 90k tokens
	longText := strings.Repeat("A", 32000) // approx 10k tokens
	for i := 0; i < 9; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: longText}}})
		_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response"}}})
	}

	// Channel to capture the log event
	logReceived := make(chan string, 1)
	a.Subscribe(func(e events.Event) {
		if msg, ok := e.(events.SystemMessageEvent); ok {
			if strings.Contains(msg.Message, "Auto-summarizing") {
				logReceived <- msg.Message
			}
		}
	})

	// Trigger the pipeline via Prepare
	_, _, err = a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify the log was emitted and contains expected data
	select {
	case msg := <-logReceived:
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
	hManager := history.NewManager(filepath.Join(tmpDir, "history_sys.json"))
	reg := registry.New()
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Summary"}}}},
			},
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	// Set initial system instructions
	client, _ := api.NewClient(apiURL, "test", &auth.VertexAuth{Token: "t"}, 0, "", 0, "Initial System Instruction", false)

	a := New(client, hManager, reg, security.NewSecurityManager(nil), true)
	a.SetLimits(10, 3500, 20) // Limit to trigger summarization

	// Add some turns (approx 3451 tokens with base overhead and tools)
	longText := strings.Repeat("A", 1600) // approx 500 tokens
	for i := 0; i < 6; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: longText}}})
		_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Response"}}})
	}

	// Trigger Prepare
	_, _, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	newContents := hManager.GetContents()
	// Should have summarized 6/2 = 3 turns (6 messages) replaced by 2 messages.
	// Total: 12 - 6 + 2 = 8 messages.
	if len(newContents) != 8 {
		t.Errorf("expected 8 messages, got %d", len(newContents))
	}

	// First message should be the auto-summary
	if !strings.Contains(newContents[0].Parts[0].Text, "System Auto-Summary") {
		t.Errorf("first message should be auto-summary, got: %s", newContents[0].Parts[0].Text)
	}

	// Ensure no "system" role messages in history (system instructions are client-side)
	for _, c := range newContents {
		if c.Role == "system" {
			t.Errorf("found system role message in history, which should be avoided")
		}
	}
}

func TestToolInjectedTokenBudgetPressure(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "tool_pressure_history.json"))
	reg := registry.New()

	// 1. Register many tools to create a large schema (approx 2000 tokens)
	for i := 0; i < 20; i++ {
		reg.Register(&tools.ToolDeclaration{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: "A tool with a very long description to consume more tokens in the schema " + strings.Repeat("detail ", 20),
			Parameters:  &tools.Schema{Type: "OBJECT"},
		}, nil)
	}

	// 2. Mock server for summarization
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Compressed history summary"}}}},
			},
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, _ := api.NewClient(apiURL, "test", &auth.VertexAuth{Token: "t"}, 0, "", 0, "", false)

	sm := security.NewSecurityManager(nil)
	a := New(client, hManager, reg, sm, true)

	// 3. Set a tight token limit.
	// Base overhead ~300.
	// Tools ~2000.
	// Total overhead ~2300.
	// Set limit to 3000.
	a.SetLimits(10, 3000, 20)

	// 4. Add history (600 tokens)
	// Total = 2300 (overhead) + 600 (history) = 2900.
	// 90% of 3000 = 2700.
	// 2900 > 2700, so it should trigger summarization.
	ctx := context.Background()
	longText := strings.Repeat("B", 1920) // approx 600 tokens
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: longText}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Short response"}}})

	// Add more turns to have something to summarize (need at least 10 messages for auto-summarize)
	for i := 0; i < 4; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Turn message"}}})
		_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}})
	}
	// Total messages = 2 + 8 = 10.

	// 5. Call Prepare. It should trigger auto-summarize because of tool schema injection.
	_, metadata, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	if !metadata.SummarizationAttempted {
		t.Errorf("Expected auto-summarization to be attempted due to tool schema token pressure, but it wasn't.")
	}

	// Verify that the resulting history is shorter
	newContents := hManager.GetContents()
	if len(newContents) >= 10 {
		t.Errorf("Expected history to be pruned/summarized, but still have %d messages", len(newContents))
	}
}
