package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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
		{"Limit 1, History 3 (Odd, Overflow)", 1, 3, 1, 1},
		{"Limit 2, History 5 (Odd, Overflow)", 2, 5, 1, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &SlidingWindowPolicy{MaxTurns: tt.maxTurns}
			history := make([]*llm.Content, tt.historyLen)
			for i := range history {
				history[i] = &llm.Content{Role: "user"}
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
				History: []*llm.Content{{Role: "user"}},
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

type mockEstimator struct {
	tokens int
}

func (m *mockEstimator) EstimateTokens(contents []*llm.Content) int {
	return m.tokens
}
