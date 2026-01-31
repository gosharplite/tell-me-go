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
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

func TestAgent_SummarizeHistory(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()
	ctx := context.Background()

	// Fill history with some turns
	for i := 1; i <= 5; i++ {
		hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{Text: "Turn User"}}})
		hManager.AddContent(ctx, &types.Content{Role: "model", Parts: []*types.Part{{Text: "Turn Model"}}})
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

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm, false)

	t.Setenv("TELL_ME_NO_STREAM", "true")

	args := map[string]interface{}{
		"turns": float64(3),
	}
	resp, err := a.ctxManager.SummarizeHistoryTool(ctx, args)
	if err != nil {
		t.Fatalf("summarizeHistory failed: %v", err)
	}

	t.Logf("Response: %s", resp)

	contents := hManager.GetContents()
	// Initial: 10 messages (5 turns)
	// Summarized 3 turns (6 messages) replaced by 2 messages.
	// Remaining: 10 - 6 + 2 = 6 messages.
	if len(contents) != 6 {
		t.Errorf("expected 6 messages in history, got %d", len(contents))
	}

	if contents[0].Parts[0].Text != "System Summary of previous context:\n\nThis is a summary." {
		t.Errorf("summary message mismatch: %s", contents[0].Parts[0].Text)
	}
}
