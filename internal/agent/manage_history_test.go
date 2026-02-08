// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"path/filepath"
	"testing"

	agentctx "github.com/gosharplite/tell-me-go/internal/agent/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
)

func TestAgent_ManageHistory(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	ctx := context.Background()

	// Fill history with 2 turns (4 messages)
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "M1"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U2"}}})
	_ = hManager.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "M2"}}})

	cm := &agentctx.ContextManager{History: hManager}
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
				contents := hManager.GetContents()
				if contents[idx*2].Pinned != tt.wantStatus || contents[idx*2+1].Pinned != tt.wantStatus {
					t.Errorf("expected pinned status %v for turn %d, got %v", tt.wantStatus, idx, contents[idx*2].Pinned)
				}
			}
		})
	}
}
