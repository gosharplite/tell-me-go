// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type mockHistoryManager struct {
	ReplaceRangeFunc func(ctx context.Context, start, end int, newContents []*llm.Content) error
}

func (m *mockHistoryManager) ReplaceRange(ctx context.Context, start, end int, newContents []*llm.Content) error {
	return m.ReplaceRangeFunc(ctx, start, end, newContents)
}

func TestSlidingWindowPolicy_Prune(t *testing.T) {
	tests := []struct {
		name         string
		maxTurns     int
		historyLen   int // Number of messages
		expectPruned int // Number of turns (2 msgs per turn)
		expectRemain int // Number of messages remaining
	}{
		{"No pruning needed", 10, 4, 0, 4},
		{"Exact limit", 5, 10, 0, 10},
		{"Pruning exceeding", 2, 10, 3, 4},  // maxTurns 2 (4 msgs). remove 10-4=6. pruned 3.
		{"Odd history length", 5, 11, 1, 9}, // 11 > 10. target 10. remove 11-10=1. remove+1=2. remain 9. pruned 1.
		{"Zero turns", 0, 10, 0, 10},
		{"Negative turns", -1, 10, 0, 10},
		{"Large history small limit", 1, 20, 9, 2}, // target 2. remove 20-2=18. pruned 9.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &SlidingWindowPolicy{MaxTurns: tt.maxTurns}
			h := make([]*llm.Content, tt.historyLen)
			for i := range h {
				h[i] = &llm.Content{Role: "user"}
			}

			gotHistory, pruned := p.Prune(context.Background(), h)
			if pruned != tt.expectPruned {
				t.Errorf("expected pruned %d, got %d", tt.expectPruned, pruned)
			}
			if len(gotHistory) != tt.expectRemain {
				t.Errorf("expected remaining %d, got %d", tt.expectRemain, len(gotHistory))
			}
		})
	}
}

func TestHistoryPruner_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("Pruning occurred", func(t *testing.T) {
		managerCalled := false
		m := &mockHistoryManager{
			ReplaceRangeFunc: func(ctx context.Context, start, end int, newContents []*llm.Content) error {
				managerCalled = true
				if start != 0 || end != 4 || newContents != nil {
					t.Errorf("unexpected ReplaceRange call: %d, %d, %v", start, end, newContents)
				}
				return nil
			},
		}
		pruner := &HistoryPruner{
			Policy:  &SlidingWindowPolicy{MaxTurns: 1}, // Max 2 msgs
			Manager: m,
		}

		req := &ContextRequest{
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "3"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "4"}}},
				{Role: "user", Parts: []*llm.Part{{Text: "5"}}},
				{Role: "model", Parts: []*llm.Part{{Text: "6"}}},
			},
		}

		err := pruner.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		if !managerCalled {
			t.Error("Manager.ReplaceRange was not called")
		}
		if len(req.History) != 2 {
			t.Errorf("expected 2 messages remaining, got %d", len(req.History))
		}
		if req.Metadata.PrunedTurns != 2 {
			t.Errorf("expected 2 pruned turns, got %d", req.Metadata.PrunedTurns)
		}
	})

	t.Run("No pruning", func(t *testing.T) {
		pruner := &HistoryPruner{
			Policy: &SlidingWindowPolicy{MaxTurns: 10},
		}
		req := &ContextRequest{
			History: []*llm.Content{{Role: "user"}},
		}
		err := pruner.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.PrunedTurns != 0 {
			t.Errorf("expected 0 pruned turns, got %d", req.Metadata.PrunedTurns)
		}
	})
}

type mockSummarizer struct {
	summarizeFn func(ctx context.Context, subset []*llm.Content, focus string) (string, error)
}

func (m *mockSummarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
	return m.summarizeFn(ctx, subset, focus)
}

func TestTokenGatekeeper_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("Under limit", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 500},
		}
		req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
		err := tg.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.FinalTokenCount != 500 {
			t.Errorf("expected 500 tokens, got %d", req.Metadata.FinalTokenCount)
		}
	})

	t.Run("Exceeds limit after summarization", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 1100}, // Always returns 1100
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
					return "summary", nil
				},
			},
			Manager: &mockHistoryManager{
				ReplaceRangeFunc: func(ctx context.Context, start, end int, newContents []*llm.Content) error {
					return nil
				},
			},
		}
		// 10 messages to allow summarization trigger (>= 10)
		h := make([]*llm.Content, 10)
		req := &ContextRequest{History: h}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, llm.ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded, got %v", err)
		}
	})

	t.Run("Summarization failure", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 950},
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
					return "", errors.New("summarize error")
				},
			},
		}
		h := make([]*llm.Content, 10)
		req := &ContextRequest{History: h}
		err := tg.Transform(ctx, req)
		// Should still succeed if under limit, but metadata won't show summarization
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}
		if req.Metadata.SummarizedTurns != 0 {
			t.Errorf("expected 0 summarized turns on error, got %d", req.Metadata.SummarizedTurns)
		}
	})
}

func TestWarningInjector_Transform(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(&mockToolRegistry{})
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject turn warning", func(t *testing.T) {
		req := &ContextRequest{
			Turn: 7, // 3 remaining
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}},
			},
		}
		req.Metadata.FinalTokenCount = 100

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		if len(req.Metadata.Warnings) == 0 {
			t.Error("expected warnings in metadata")
		}
		lastContent := req.History[len(req.History)-1]
		if !strings.Contains(lastContent.Parts[len(lastContent.Parts)-1].Text, "3 turns remaining") {
			t.Errorf("warning not found in content: %v", lastContent.Parts)
		}
	})
}
