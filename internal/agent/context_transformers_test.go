// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type mockHistoryManager struct {
	ReplaceRangeFunc func(ctx context.Context, start, end int, newContents []*types.Content) error
}

func (m *mockHistoryManager) ReplaceRange(ctx context.Context, start, end int, newContents []*types.Content) error {
	return m.ReplaceRangeFunc(ctx, start, end, newContents)
}

func TestSlidingWindowPolicy_Prune(t *testing.T) {
	tests := []struct {
		name          string
		maxTurns      int
		historyLen    int // Number of messages
		expectPruned  int // Number of turns (2 msgs per turn)
		expectRemain  int // Number of messages remaining
	}{
		{"No pruning needed", 10, 4, 0, 4},
		{"Exact limit", 5, 10, 0, 10},
		{"Pruning exceeding", 2, 10, 4, 2}, // maxTurns 2 (4 msgs). targetMessages (2/2)*2 = 2 msgs. Pruned (10-2)/2 = 4 turns.
		{"Odd history length", 5, 11, 4, 3}, // 11 > 10. target (5/2)*2 = 4. remove = 11-4 = 7. remove+1 = 8. remain = 3. pruned = 4.
		{"Zero turns", 0, 10, 0, 10},
		{"Negative turns", -1, 10, 0, 10},
		{"Large history small limit", 1, 20, 9, 2}, // targetMessages (1/2)*2 = 0, clamped to 2. Pruned (20-2)/2 = 9 turns.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &SlidingWindowPolicy{MaxTurns: tt.maxTurns}
			history := make([]*types.Content, tt.historyLen)
			for i := range history {
				history[i] = &types.Content{Role: "user"}
			}

			gotHistory, pruned := p.Prune(context.Background(), history)
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
			ReplaceRangeFunc: func(ctx context.Context, start, end int, newContents []*types.Content) error {
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
			History: []*types.Content{
				{Role: "user", Parts: []*types.Part{{Text: "1"}}},
				{Role: "model", Parts: []*types.Part{{Text: "2"}}},
				{Role: "user", Parts: []*types.Part{{Text: "3"}}},
				{Role: "model", Parts: []*types.Part{{Text: "4"}}},
				{Role: "user", Parts: []*types.Part{{Text: "5"}}},
				{Role: "model", Parts: []*types.Part{{Text: "6"}}},
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
			History: []*types.Content{{Role: "user"}},
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

type mockEstimator struct {
	tokens int
}

func (m *mockEstimator) EstimateTokens(contents []*types.Content) int {
	return m.tokens
}

type mockSummarizer struct {
	summarizeFn func(ctx context.Context, subset []*types.Content, focus string) (string, error)
}

func (m *mockSummarizer) Summarize(ctx context.Context, subset []*types.Content, focus string) (string, error) {
	return m.summarizeFn(ctx, subset, focus)
}

func TestTokenGatekeeper_Transform(t *testing.T) {
	ctx := context.Background()

	t.Run("Under limit", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 500},
		}
		req := &ContextRequest{History: []*types.Content{{Role: "user"}}}
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
				summarizeFn: func(ctx context.Context, subset []*types.Content, focus string) (string, error) {
					return "summary", nil
				},
			},
			Manager: &mockHistoryManager{
				ReplaceRangeFunc: func(ctx context.Context, start, end int, newContents []*types.Content) error {
					return nil
				},
			},
		}
		// 10 messages to allow summarization trigger (>= 10)
		history := make([]*types.Content, 10)
		req := &ContextRequest{History: history}
		err := tg.Transform(ctx, req)
		if !errors.Is(err, ErrContextLimitExceeded) {
			t.Errorf("expected ErrContextLimitExceeded, got %v", err)
		}
	})

	t.Run("Summarization failure", func(t *testing.T) {
		tg := &TokenGatekeeper{
			MaxTokens: 1000,
			Estimator: &mockEstimator{tokens: 950},
			Summarizer: &mockSummarizer{
				summarizeFn: func(ctx context.Context, subset []*types.Content, focus string) (string, error) {
					return "", errors.New("summarize error")
				},
			},
		}
		history := make([]*types.Content, 10)
		req := &ContextRequest{History: history}
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
	strategy := NewContextStrategy(&mockRegistry{})
	strategy.SetLimits(1000, 10, 20)

	injector := &WarningInjector{Strategy: strategy}

	t.Run("Inject turn warning", func(t *testing.T) {
		req := &ContextRequest{
			Turn: 7, // 3 remaining
			Result: []*types.Content{
				{Role: "user", Parts: []*types.Part{{Text: "prompt"}}},
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
		lastContent := req.Result[len(req.Result)-1]
		if !strings.Contains(lastContent.Parts[len(lastContent.Parts)-1].Text, "3 turns remaining") {
			t.Errorf("warning not found in content: %v", lastContent.Parts)
		}
	})
}

func TestContextManager_Prepare_PipelineIntegration(t *testing.T) {
	ctx := context.Background()
	reg := &mockRegistry{}
	strategy := NewContextStrategy(reg)
	strategy.SetLimits(1000, 10, 5) // Max 10 messages (5 turns)

	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	
	// Add 12 messages (6 turns) to trigger pruning
	for i := 0; i < 6; i++ {
		_ = hManager.AddContent(ctx, &types.Content{Role: "user", Parts: []*types.Part{{Text: "u"}}})
		_ = hManager.AddContent(ctx, &types.Content{Role: "model", Parts: []*types.Part{{Text: "m"}}})
	}

	cm := NewContextManager(strategy, hManager, &mockGateway{
		generateFn: func(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (<-chan *types.Content, func() (*types.Content, *types.Metrics, error)) {
			ch := make(chan *types.Content)
			close(ch)
			return ch, func() (*types.Content, *types.Metrics, error) {
				return &types.Content{Parts: []*types.Part{{Text: "summary"}}}, &types.Metrics{}, nil
			}
		},
	}, &SimpleEventBus{})

	apiContents, metadata, err := cm.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// 1. HistoryPruner should have run
	if metadata.PrunedTurns == 0 {
		t.Error("expected pruned turns in metadata")
	}

	// 2. TokenGatekeeper should have run
	if metadata.FinalTokenCount == 0 {
		t.Error("expected final token count in metadata")
	}

	// 3. WarningInjector should have run (if turns/tokens high, but here pruned turns > 5 might trigger it)
	// strategy.GetHistoryTurnWarning(currentTurns) where prunedTurns > 5
	// In this case, 12 messages -> pruned 4 turns (8 msgs). 12-8 = 4 messages (2 turns).
	// prunedTurns = 4. Not > 5.
	// Let's force a warning by turn count.
	strategy.SetLimits(1000, 10, 100)
	apiContents, metadata, err = cm.Prepare(ctx, 9) // 1 remaining
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	
	foundWarning := false
	for _, w := range metadata.Warnings {
		if strings.Contains(w, "final turn") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected final turn warning in metadata")
	}

	lastContent := apiContents[len(apiContents)-1]
	if !strings.Contains(lastContent.Parts[len(lastContent.Parts)-1].Text, "final turn") {
		t.Error("expected final turn warning in last message")
	}
}
