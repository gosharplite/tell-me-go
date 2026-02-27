// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

func TestPruner_MidExecutionCancel(t *testing.T) {
	// 1. Initialize Context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Setup Thread-Safe Cancellation
	var cancelOnce sync.Once

	// 3. Configure the Mock
	mockPolicy := &mockPruningPolicy{
		markTurnsFn: func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
			cancelOnce.Do(cancel) // Deterministically cancel during execution
			return 0, nil
		},
	}

	// 4. Execute and Assert
	pruner := &historyPruner{
		Policy: mockPolicy,
	}

	// We need some history to trigger reconstructHistory loop
	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
		},
	}

	err := pruner.Transform(ctx, req)

	// Assert that the returned error matches context.Canceled
	require.ErrorIs(t, err, context.Canceled)
}

func TestPruner_Policies_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled context

	turns := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}},
	}
	keep := make([]bool, len(turns))

	tests := []struct {
		name   string
		policy ports.PruningPolicy
	}{
		{"SlidingWindow", &slidingWindowPolicy{MaxTurns: 10}},
		{"Importance", &importanceRankPolicy{}},
		{"Pinning", &pinningPolicy{}},
		{"Composite", &compositePruningPolicy{Policies: []ports.PruningPolicy{&slidingWindowPolicy{MaxTurns: 10}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.policy.MarkTurns(ctx, turns, keep)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 0, count)
		})
	}
}
