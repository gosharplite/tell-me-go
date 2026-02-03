// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
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
				if start != 0 || end != 6 || len(newContents) != 2 {
					t.Errorf("unexpected ReplaceRange call: start=%d, end=%d, len=%d", start, end, len(newContents))
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
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
		req := &ContextRequest{History: h}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, llm.ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded, got %v", err)
		}
	})

	t.Run("Summarization failure", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 2000,
			Estimator: &mockEstimator{tokens: 950},
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
					return "", errors.New("summarize error")
				},
			},
		}
		h := make([]*llm.Content, 10)
		for i := range h {
			h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
		}
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
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject turn warning", func(t *testing.T) {
		req := &ContextRequest{
			Turn: 8, // 2 remaining
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
		if !strings.Contains(lastContent.Parts[len(lastContent.Parts)-1].Text, "Only 2 turns remain") {
			t.Errorf("warning not found in content: %v", lastContent.Parts)
		}
	})
}

func TestSlidingWindowPolicy_Prune_Pinned(t *testing.T) {
	t.Parallel()
	p := &SlidingWindowPolicy{MaxTurns: 1} // Keep 1 turn (2 messages)

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "T1 User"}}, Pinned: true},
		{Role: "model", Parts: []*llm.Part{{Text: "T1 Model"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "T2 User"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "T2 Model"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "T3 User"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "T3 Model"}}},
	}

	// Without pinning, it would keep only T3.
	// With T1 pinned, it should keep T1 AND T3.
	gotHistory, pruned := p.Prune(context.Background(), history)

	if pruned != 1 {
		t.Errorf("expected 1 pruned turn (T2), got %d", pruned)
	}

	if len(gotHistory) != 4 {
		t.Fatalf("expected 4 messages (T1 and T3), got %d", len(gotHistory))
	}

	if gotHistory[0].Parts[0].Text != "T1 User" {
		t.Errorf("expected T1 User as first message, got %q", gotHistory[0].Parts[0].Text)
	}
	if gotHistory[2].Parts[0].Text != "T3 User" {
		t.Errorf("expected T3 User as third message, got %q", gotHistory[2].Parts[0].Text)
	}
}

func TestSlidingWindowPolicy_Prune_Pinned_ModelPart(t *testing.T) {
	t.Parallel()
	p := &SlidingWindowPolicy{MaxTurns: 1} // Keep 1 turn

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "T1 User"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "T1 Model"}}, Pinned: true}, // Pin the model part
		{Role: "user", Parts: []*llm.Part{{Text: "T2 User"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "T2 Model"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "T3 User"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "T3 Model"}}},
	}

	gotHistory, pruned := p.Prune(context.Background(), history)

	if pruned != 1 {
		t.Errorf("expected 1 pruned turn (T2), got %d", pruned)
	}

	if len(gotHistory) != 4 {
		t.Fatalf("expected 4 messages (T1 and T3), got %d", len(gotHistory))
	}
}

type dynamicMockEstimator struct {
	tokens int
}

func (m *dynamicMockEstimator) EstimateTokens(contents []*llm.Content) int {
	// If it contains a summary, return less tokens
	for _, c := range contents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "System Auto-Summary") {
				return 500
			}
		}
	}
	return m.tokens
}

func TestTokenGatekeeper_AutoSummarize_PinnedAware(t *testing.T) {
	ctx := context.Background()

	summarizerCalled := false
	summarizer := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, error) {
			summarizerCalled = true
			return "summary", nil
		},
	}

	managerCalled := false
	manager := &mockHistoryManager{
		ReplaceRangeFunc: func(ctx context.Context, start, end int, newContents []*llm.Content) error {
			managerCalled = true
			return nil
		},
	}

	tg := &TokenGatekeeper{
		MaxTokens:  10000,
		Estimator:  &dynamicMockEstimator{tokens: 9500},
		Summarizer: summarizer,
		Manager:    manager,
	}

	// Create 10 turns (20 messages)
	h := make([]*llm.Content, 20)
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}
		h[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: fmt.Sprintf("Msg %d", i)}}}
	}

	// Pin Turn 0 and Turn 1
	h[0].Pinned = true
	h[1].Pinned = true
	h[2].Pinned = true
	h[3].Pinned = true

	req := &ContextRequest{History: h}

	err := tg.Transform(ctx, req)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if !summarizerCalled {
		t.Error("Summarizer was not called")
	}
	if !managerCalled {
		t.Error("Manager.ReplaceRange was not called")
	}

	// Verify pinned turns still exist at the beginning of req.History
	if !req.History[0].Pinned || req.History[0].Parts[0].Text != "Msg 0" {
		t.Error("Turn 0 (pinned) was lost or corrupted")
	}
	if !req.History[2].Pinned || req.History[2].Parts[0].Text != "Msg 2" {
		t.Error("Turn 1 (pinned) was lost or corrupted")
	}
}

func TestWarningInjector_Transform_Clogged(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject clogged warning", func(t *testing.T) {
		req := &ContextRequest{
			Turn: 1,
			History: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}},
			},
		}
		req.Metadata.FinalTokenCount = 860 // > 85% of 1000
		req.Metadata.SummarizationAttempted = true

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		if len(req.Metadata.Warnings) == 0 {
			t.Error("expected warnings in metadata")
		}
		lastContent := req.History[len(req.History)-1]
		found := false
		for _, p := range lastContent.Parts {
			if strings.Contains(p.Text, "A recent summarization failed to significantly reduce context size") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("clogged warning not found in content parts: %v", lastContent.Parts)
		}
	})
}

func TestTokenGatekeeper_SetsSummarizationAttempted(t *testing.T) {
	ctx := context.Background()
	tg := &TokenGatekeeper{
		MaxTokens: 10000,
		Estimator: &dynamicMockEstimator{tokens: 9500},
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
	h := make([]*llm.Content, 10)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	req := &ContextRequest{History: h}
	err := tg.Transform(ctx, req)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if !req.Metadata.SummarizationAttempted {
		t.Error("expected SummarizationAttempted to be true")
	}
}

func TestTokenGatekeeper_AutoSummarize_BlockedByPins(t *testing.T) {
	ctx := context.Background()
	tg := &TokenGatekeeper{
		MaxTokens: 2000,
		Estimator: &mockEstimator{tokens: 1900}, // > 90%
	}

	// Create history where all messages are pinned
	h := make([]*llm.Content, 20)
	for i := range h {
		h[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}, Pinned: true}
	}
	req := &ContextRequest{History: h}

	err := tg.Transform(ctx, req)
	// autoSummarize will fail, but since tokens (1900) < SafetyLimit (2000-buffer), it might not fail the turn.
	// Wait, MT=2000. Buffer=1000. SafetyLimit = 1000. 1900 > 1000. So it WILL fail with ErrContextLimitExceeded.
	if !errors.Is(err, llm.ErrContextLimitExceeded) {
		t.Fatalf("expected ErrContextLimitExceeded, got %v", err)
	}

	if !req.Metadata.MaintenanceBlocked {
		t.Error("expected MaintenanceBlocked to be true")
	}
}

func TestWarningInjector_Transform_MaintenanceBlocked(t *testing.T) {
	ctx := context.Background()
	strategy := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Blocked triggers clogged warning", func(t *testing.T) {
		req := &ContextRequest{
			History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}}},
		}
		req.Metadata.FinalTokenCount = 900 // > 85%
		req.Metadata.MaintenanceBlocked = true

		err := injector.Transform(ctx, req)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		found := false
		for _, w := range req.Metadata.Warnings {
			if strings.Contains(w, "unpin non-essential turns using 'manage_history' (unpin)") {
				found = true
				break
			}
		}
		if !found {
			t.Error("Clogged warning not found in metadata after maintenance was blocked")
		}
	})
}

func TestTokenGatekeeper_SafetyBuffer_Boundary(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		maxTokens int
		tokens    int
		wantErr   bool
	}{
		{"Small context, under limit", 100, 80, false},
		{"Small context, over safety limit (90)", 100, 95, true},
		{"Medium context, under limit", 1000, 850, false},
		{"Medium context, over safety limit (900)", 1000, 950, true},
		{"Large context, under limit", 100000, 98000, false},
		{"Large context, over safety limit (99000)", 100000, 99500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := &TokenGatekeeper{
				MaxTokens: tt.maxTokens,
				Estimator: &mockEstimator{tokens: tt.tokens},
			}
			req := &ContextRequest{History: []*llm.Content{{Role: "user"}}}
			err := tg.Transform(ctx, req)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr = %v, got %v", tt.wantErr, err)
			}
		})
	}
}
