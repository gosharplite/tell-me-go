package session

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestTokenGatekeeper_Table(t *testing.T) {
	tests := []struct {
		name        string
		maxTokens   int
		tokens      int
		shouldError bool
	}{
		{"Well under limit", 2000, 500, false},
		{"Exactly at safety limit", 2000, 1800, false}, // limit = 2000 - 10% = 1800
		{"Over safety limit", 2000, 1900, true},        // 1900 > 1800
		{"Zero limit", 0, 1, false},                    // MaxTokens 0 means no limit
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg := &tokenGatekeeper{
				MaxTokens: tt.maxTokens,
				Estimator: &mockEstimator{tokens: tt.tokens},
			}
			req := &request{
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
