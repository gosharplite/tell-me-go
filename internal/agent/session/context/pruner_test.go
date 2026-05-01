// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
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
	mockPolicy := &agenttest.MockPruningPolicy{
		MarkTurnsFn: func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
			cancelOnce.Do(cancel) // Deterministically cancel during execution
			return 0, nil
		},
	}

	// 4. Execute and Assert
	pruner := &HistoryPruner{
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
		{"SlidingWindow", &SlidingWindowPolicy{MaxTurns: 10}},
		{"Importance", &importanceRankPolicy{}},
		{"Pinning", &pinningPolicy{}},
		{"Composite", &compositePruningPolicy{Policies: []ports.PruningPolicy{&SlidingWindowPolicy{MaxTurns: 10}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.policy.MarkTurns(ctx, turns, keep)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 0, count)
		})
	}
}

func TestPruner_ApplyPolicies_ErrorPropagation(t *testing.T) {
	// 1. Create a mock pruning policy that returns an error
	mockErr := errors.New("mock policy failure")
	mockPolicy := &agenttest.MockPruningPolicy{
		MarkTurnsFn: func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
			return 0, mockErr
		},
	}

	// 2. Instantiate Pruner (HistoryPruner)
	pruner := &HistoryPruner{
		Policy: mockPolicy,
	}

	// 3. Setup Request with history to trigger policy call
	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "world"}}},
		},
	}

	// 4. Execute Transform
	err := pruner.Transform(context.Background(), req)

	// 5. Assert exact error propagation
	require.ErrorIs(t, err, mockErr)
	require.Contains(t, err.Error(), "mock policy failure")
}

func TestPruner_CompositePolicy_ErrorPropagation(t *testing.T) {
	// 1. Create a sub-policy that returns an error
	mockErr := errors.New("composite sub-policy failure")
	mockSubPolicy := &agenttest.MockPruningPolicy{
		MarkTurnsFn: func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
			return 0, mockErr
		},
	}

	// 2. Create Composite Policy
	composite := &compositePruningPolicy{
		Policies: []ports.PruningPolicy{mockSubPolicy},
	}

	// 3. Instantiate Pruner
	pruner := &HistoryPruner{
		Policy: composite,
	}

	// 4. Setup Request
	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}

	// 5. Execute
	err := pruner.Transform(context.Background(), req)

	// 6. Assert exact error propagation
	require.ErrorIs(t, err, mockErr)
	require.Contains(t, err.Error(), "composite sub-policy failure")
}

func TestCompositePruningPolicy_MarkTurns_ErrorPropagation(t *testing.T) {
	// Although HistoryPruner handles composites specifically, the MarkTurns method
	// of compositePruningPolicy also has an error path that should be tested.
	mockErr := errors.New("composite direct call failure")
	mockSubPolicy := &agenttest.MockPruningPolicy{
		MarkTurnsFn: func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
			return 0, mockErr
		},
	}

	composite := &compositePruningPolicy{
		Policies: []ports.PruningPolicy{mockSubPolicy},
	}

	turns := [][]*llm.Content{{{Role: "user"}}}
	keep := make([]bool, 1)

	_, err := composite.MarkTurns(context.Background(), turns, keep)
	require.ErrorIs(t, err, mockErr)
}
