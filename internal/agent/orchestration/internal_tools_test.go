// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestAgent_ManageHistory(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// Fill history with 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "M1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U2"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "M2"}}})

	cm := &ContextManager{History: hManager}
	it := NewInternalTools(cm)

	tests := []struct {
		name        string
		action      string
		index       float64
		expectedErr bool
		wantStatus  bool
	}{
		{
			name:       "pin turn 0",
			action:     "pin",
			index:      0,
			wantStatus: true,
		},
		{
			name:       "unpin turn 0",
			action:     "unpin",
			index:      0,
			wantStatus: false,
		},
		{
			name:       "pin turn 1",
			action:     "pin",
			index:      1,
			wantStatus: true,
		},
		{
			name:        "invalid action",
			action:      "delete",
			index:       0,
			expectedErr: true,
		},
		{
			name:        "invalid index",
			action:      "pin",
			index:       2,
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
					args := map[string]interface{}{
				"action": tt.action,
				"index":  tt.index,
			}
			_, err := it.ManageHistory(ctx, args)

			if (err != nil) != tt.expectedErr {
				t.Fatalf("expected error: %v, got: %v", tt.expectedErr, err)
			}

			if !tt.expectedErr {
				idx := int(tt.index)
				contents, _ := hManager.GetWindow(ctx, 0, -1)
				if contents[idx*2].Pinned != tt.wantStatus || contents[idx*2+1].Pinned != tt.wantStatus {
					t.Errorf("expected pinned status %v for turn %d, got %v", tt.wantStatus, idx, contents[idx*2].Pinned)
				}
			}
		})
	}
}

func TestRegisterInternal(t *testing.T) {
	registry := &mockToolRegistry{}
	cm := &ContextManager{}
	RegisterInternal(registry, cm)

	decls := registry.GetDeclarations()
	if len(decls) != 2 {
		t.Fatalf("expected 2 tools registered, got %d", len(decls))
	}

	// Create an expectation map: tool name -> required parameters
	expectedTools := map[string][]string{
		"summarize_history": {"turns", "focus"},
		"manage_history":    {"action", "index"},
	}

	for _, d := range decls {
		params, ok := expectedTools[d.Name]
		if !ok {
			t.Errorf("Unexpected tool registered: %s", d.Name)
			continue
		}

		// Generic structural tests for all valid tools
		if d.Description == "" {
			t.Errorf("Tool %s missing description", d.Name)
		}
		if d.Parameters == nil || d.Parameters.Type != "OBJECT" {
			t.Errorf("Tool %s missing or invalid parameters", d.Name)
			continue
		}

		// Check for specific parameters
		for _, p := range params {
			if _, ok := d.Parameters.Properties[p]; !ok {
				t.Errorf("Tool %s missing '%s' parameter", d.Name, p)
			}
		}

		// Mark as found
		delete(expectedTools, d.Name)
	}

	// Assert no missing tools
	if len(expectedTools) > 0 {
		t.Errorf("Failed to register expected tools: %v", expectedTools)
	}
}

func TestInternalTools_SummarizeHistory(t *testing.T) {
	mockSumm := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return "summary result", &llm.Metrics{ResponseTokens: 10}, nil
		},
	}
	factory := &PipelineFactory{
		Summarizer: mockSumm,
		Estimator:  NewContextStrategy(&mockTokenCounter{}, &mockEventBus{}),
		Events:     &mockEventBus{},
	}
	hManager := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "U1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "M1"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "U2"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "M2"}}},
		},
	}
	cm := NewContextManager(NewContextStrategy(&mockTokenCounter{}, &mockEventBus{}), hManager, &mockEventBus{}, factory)
	it := NewInternalTools(cm)

	ctx := context.Background()

	t.Run("valid summarization", func(t *testing.T) {
			args := map[string]interface{}{
			"turns": 1.0,
			"focus": "test focus",
		}
		res, err := it.summarizeHistory(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Text != "Summarized the first 1 turns of history." {
			t.Errorf("expected text 'Summarized the first 1 turns of history.', got '%s'", res.Text)
		}
		metrics := res.Metadata["metrics"].(*llm.Metrics)
		if metrics.ResponseTokens != 10 {
			t.Errorf("expected 10 response tokens, got %d", metrics.ResponseTokens)
		}
	})

	t.Run("invalid turns", func(t *testing.T) {
			args := map[string]interface{}{
			"turns": 0.0,
		}
		_, err := it.summarizeHistory(ctx, args)
		if err == nil {
			t.Fatal("expected error for 0 turns, got nil")
		}
	})

	t.Run("missing arguments", func(t *testing.T) {
			args := map[string]interface{}{}
		_, err := it.summarizeHistory(ctx, args)
		if err == nil {
			t.Fatal("expected error for missing arguments, got nil")
		}
	})
}
