// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// MockPruningPolicy is a test double for ports.PruningPolicy. The
// default MarkTurns is a no-op and Name returns "MockPolicy"; override
// the *Fn fields to script behaviour.
type MockPruningPolicy struct {
	MarkTurnsFn func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error)
	NameFn      func() string
}

func (m *MockPruningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	if m.MarkTurnsFn != nil {
		return m.MarkTurnsFn(ctx, turns, keep)
	}
	return 0, nil
}

func (m *MockPruningPolicy) Name() string {
	if m.NameFn != nil {
		return m.NameFn()
	}
	return "MockPolicy"
}
