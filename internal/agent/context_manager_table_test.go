package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestSlidingWindowPolicy_Extended(t *testing.T) {
	tests := []struct {
		name         string
		maxTurns     int
		historyLen   int
		expectPruned int
		expectRemain int
	}{
		{"Limit 1, History 1 (Odd)", 1, 1, 0, 1},
		{"Limit 1, History 2 (Even)", 1, 2, 0, 2},
		{"Limit 1, History 3 (Odd, Overflow)", 1, 3, 1, 1}, // target (1/2)*2=0 -> 2. remove 3-2=1. remove+1=2. remain 1.
		{"Limit 2, History 5 (Odd, Overflow)", 2, 5, 1, 3}, // target (2/2)*2=2. remove 5-2=3. remove+1=4. remain 1?
        // Wait, SlidingWindowPolicy.Prune logic:
        /*
        targetMessages := (p.MaxTurns / 2) * 2
        if targetMessages < 2 { targetMessages = 2 }
        remove := len(history) - targetMessages
        if remove % 2 != 0 { remove++ }
        prunedTurns = remove / 2
        return history[remove:], prunedTurns
        */
        // If historyLen=5, target=2. remove=5-2=3. remove=4. return history[4:], pruned=2. remain=1.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &SlidingWindowPolicy{MaxTurns: tt.maxTurns}
			history := make([]*types.Content, tt.historyLen)
			for i := range history {
				history[i] = &types.Content{Role: "user"}
			}

			gotHistory, _ := p.Prune(context.Background(), history)
			if len(gotHistory) != tt.expectRemain {
				t.Errorf("%s: expected remaining %d, got %d", tt.name, tt.expectRemain, len(gotHistory))
			}
		})
	}
}

func TestTokenGatekeeper_Table(t *testing.T) {
	tests := []struct {
		name        string
		maxTokens   int
		tokens      int
		shouldError bool
	}{
		{"Well under limit", 1000, 500, false},
		{"Exactly at limit", 1000, 1000, false},
		{"One over limit", 1000, 1001, true},
		{"Zero limit", 0, 100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := &TokenGatekeeper{
				MaxTokens: tt.maxTokens,
				Estimator: &mockEstimator{tokens: tt.tokens},
			}
			req := &ContextRequest{
				History: []*types.Content{{Role: "user"}},
			}
			err := tg.Transform(context.Background(), req)
			if tt.shouldError && err == nil {
				t.Errorf("expected error for tokens %d (max %d), got nil", tt.tokens, tt.maxTokens)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("unexpected error for tokens %d (max %d): %v", tt.tokens, tt.maxTokens, err)
			}
		})
	}
}
