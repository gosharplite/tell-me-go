// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
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
	// Initial 18 messages. Instruction turn (2 msgs) prepended to req.History.
	// Turn 0 (Instr + Ack) is pinned.
	// Turns 1-5 (10 messages in req.History, which are Msg 0-9 in hManager) are summarized.
	// 10 messages replaced by 2.
	// Total: 18 - 10 + 2 = 10 messages.
	if len(newContents) != 10 {
		t.Errorf("expected 10 messages after auto-summarization, got %d", len(newContents))
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
