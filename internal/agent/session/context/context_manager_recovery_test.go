// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context_test

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/require"
)

// TestManager_Prepare_RecoveryFromOverflow_ForcesSummarization verifies the
// ADR-059 signal end-to-end at Manager level, built on the Q4-c shape
// (TestManager_Prepare_ReSummarizesOnSecondPrepare, context_manager_test.go:534):
// with a fixed token counter UNDER the 90% pressure threshold
// (12000 < 18000 = 90% of 20000), a bare Prepare does not summarise, while a
// Prepare with WithOverflowRecovery() forces summarization and returns a
// strictly smaller window (24 → 14 messages).
func TestManager_Prepare_RecoveryFromOverflow_ForcesSummarization(t *testing.T) {
	tc := &agenttest.MockTokenCounter{Tokens: 12000}
	strategy := sessctx.NewStrategy(tc)

	mockSum := &agenttest.MockSummarizer{}
	var summarizeCalls int
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		summarizeCalls++
		return "summary", nil, nil
	})

	// WIRING ORDER IS LOAD-BEARING: both must happen BEFORE NewManager.
	// SetLimits(20000, 100, 200) — MaxHistoryTokens=20000, MaxToolTurns=100,
	// MaxHistoryTurns=200 (pruner active but non-trimming: 12 turns ≪ 200).
	// NewManager builds the pipeline from GetLimits() and wires cm.Summarizer
	// from the factory — a nil factory summarizer would make the gatekeeper
	// return a blocking ErrTerminal and the test would fail for the wrong reason.
	strategy.SetLimits(20000, 100, 200)
	factory := &sessctx.Factory{Estimator: strategy, Summarizer: mockSum}

	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents(makePinnableTurnsExt(12))

	cm := sessctx.NewManager(strategy, history, nil, factory)

	ctx := context.Background()

	// First Prepare, bare call: 12000 < 18000 → no summarization.
	_, m1, err := cm.Prepare(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, m1)
	require.False(t, m1.SummarizationAttempted, "under-threshold bare Prepare must not summarise")
	require.Equal(t, 0, summarizeCalls)

	// Second Prepare with the recovery signal: forced summarization shrinks
	// the window from 24 messages to 14 (6 of 12 turns summarised).
	h2, m2, err := cm.Prepare(ctx, 1, sessctx.WithOverflowRecovery())
	require.NoError(t, err)
	require.NotNil(t, m2)
	require.True(t, m2.SummarizationAttempted, "recovery Prepare must force summarization")
	require.Equal(t, 6, m2.SummarizedTurns)
	require.Len(t, h2, 14, "recovery Prepare must return a strictly smaller window")
	require.Equal(t, 1, summarizeCalls)
}
