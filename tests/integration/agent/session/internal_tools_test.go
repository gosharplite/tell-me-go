// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	domain_testutil "github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
)

func TestAgent_ManageHistory(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(domain_testutil.NewOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// Fill history with 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "M1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U2"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "M2"}}})

	cm := session.NewContextManager(nil, hManager, nil, nil)
	it := session.NewInternalTools(cm)

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
			_, err := it.ManageHistory(ctx, args, nil)

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
	registry := &testutil.MockToolRegistry{}
	cm := session.NewContextManager(nil, nil, nil, nil)
	if err := session.RegisterInternal(registry, cm); err != nil {
		t.Fatalf("RegisterInternal failed: %v", err)
	}

	decls := registry.GetDeclarations()
	declsMap := make(map[string]*tools.ToolDeclaration)
	for _, d := range decls {
		declsMap[d.Name] = d
	}

	tests := []struct {
		name           string
		expectedParams []string
	}{
		{
			name:           "summarize_history",
			expectedParams: []string{"turns", "focus"},
		},
		{
			name:           "manage_history",
			expectedParams: []string{"action", "index"},
		},
	}

	if len(decls) != len(tests) {
		t.Fatalf("expected %d tools registered, got %d", len(tests), len(decls))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateTool(t, declsMap[tt.name], tt.expectedParams)
		})
	}
}

func validateTool(t *testing.T, found *tools.ToolDeclaration, expectedParams []string) {
	if found == nil {
		t.Fatalf("Tool not registered")
	}

	if found.Description == "" {
		t.Errorf("Tool missing description")
	}
	if found.Parameters == nil || found.Parameters.Type != "OBJECT" {
		t.Errorf("Tool missing or invalid parameters")
		return
	}

	for _, p := range expectedParams {
		if _, ok := found.Parameters.Properties[p]; !ok {
			t.Errorf("Tool missing '%s' parameter", p)
		}
	}
}

func TestInternalTools_SummarizeHistory(t *testing.T) {
	mockSumm := &testutil.MockSummarizer{}
	mockSumm.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "summary result", &llm.Metrics{ResponseTokens: 10}, nil
	})
	factory := &session.PipelineFactory{
		Summarizer: mockSumm,
		Estimator:  session.NewContextStrategy(&testutil.MockTokenCounter{}),
		Events:     &testutil.MockEventBus{},
	}
	hManager := &testutil.MockHistoryManager{}
	hManager.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "U1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "M1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "U2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "M2"}}},
	})
	cm := session.NewContextManager(session.NewContextStrategy(&testutil.MockTokenCounter{}), hManager, &testutil.MockEventBus{}, factory)
	it := session.NewInternalTools(cm)

	ctx := context.Background()

	t.Run("valid summarization", func(t *testing.T) {
		args := map[string]interface{}{
			"turns": 1.0,
			"focus": "test focus",
		}
		res, err := it.SummarizeHistory(ctx, args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Text != "summarized the first 1 turns of history" {
			t.Errorf("expected text 'summarized the first 1 turns of history', got '%s'", res.Text)
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
		_, err := it.SummarizeHistory(ctx, args, nil)
		if err == nil {
			t.Fatal("expected error for 0 turns, got nil")
		}
	})

	t.Run("missing arguments", func(t *testing.T) {
		args := map[string]interface{}{}
		_, err := it.SummarizeHistory(ctx, args, nil)
		if err == nil {
			t.Fatal("expected error for missing arguments, got nil")
		}
	})
}

func TestRegisterInternal_ErrorPath(t *testing.T) {
	tests := []struct {
		name      string
		failAfter int
	}{
		{
			name:      "fail on first registration",
			failAfter: 0,
		},
		{
			name:      "fail on second registration",
			failAfter: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := &testutil.MockToolRegistry{}
			registry.SetRegisterErr(fmt.Errorf("registry error"))
			registry.SetFailAfter(tt.failAfter)
			cm := session.NewContextManager(nil, nil, nil, nil)
			err := session.RegisterInternal(registry, cm)
			if err == nil {
				t.Fatalf("expected initialization to fail when registry returns an error (failAfter=%d)", tt.failAfter)
			}
		})
	}
}
