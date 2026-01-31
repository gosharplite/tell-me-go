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

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

func TestContextManager_AutoSummarizeTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()
	registry.Register(&types.ToolDeclaration{
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

	sm := tools.NewSecurityManager()
	// Disable streaming for testing convenience with httptest
	a := New(client, hManager, registry, sm, true)

	// Set a token limit to trigger auto-summarization.
	// With a 2000 token limit, the 90% threshold is 1800.
	a.SetLimits(10, 2000, 20)

	// Fill history with enough content to exceed 1800 tokens.
	// Estimation: tokens = (charCount / 3.2) to be conservative.
	// 6000 chars / 3.2 ≈ 1875 tokens, safely triggering the logic.
	longText := strings.Repeat("A", 1000)
	for i := 0; i < 6; i++ { // 12 messages
		hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{Text: longText}}})
		hManager.AddContent(ctx, &types.Content{Role: "model", Parts: []*types.Part{{Text: "Response"}}})
	}

	// Verify initial count
	initialContents := hManager.GetContents()
	if len(initialContents) != 12 {
		t.Fatalf("expected 12 messages, got %d", len(initialContents))
	}

	// Call Prepare, which should trigger AutoSummarize
	_, tokens, _, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	t.Logf("Tokens after Prepare: %d", tokens)

	// Check if history was replaced
	newContents := hManager.GetContents()
	// AutoSummarize takes (len / 4) * 2 messages. 12 / 4 = 3. 3 * 2 = 6 messages.
	// 6 messages replaced by 2.
	// Total: 12 - 6 + 2 = 8 messages.
	if len(newContents) != 8 {
		t.Errorf("expected 8 messages after auto-summarization, got %d", len(newContents))
	}

	if !strings.Contains(newContents[0].Parts[0].Text, "System Auto-Summary") {
		t.Errorf("first message should be auto-summary, got: %s", newContents[0].Parts[0].Text)
	}
}
