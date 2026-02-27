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
