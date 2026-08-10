// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/require"
)

// makeUnpinnedTurns creates n user/model turns (2n messages), all unpinned,
// with unique per-turn text. Mirrors makePinnableTurnsExt in the external
// context_test package (not visible from this in-package test).
func makeUnpinnedTurns(n int) []*llm.Content {
	var contents []*llm.Content
	for i := 0; i < n; i++ {
		contents = append(contents,
			&llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("u%d", i+1)}}},
			&llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("m%d", i+1)}}},
		)
	}
	return contents
}

// recoveryGatekeeperFixture builds a TokenGatekeeper with a fixed token
// estimator and a counting summarizer for the ADR-059 recovery tests.
// It returns the gatekeeper and a pointer to the summarizer call counter.
func recoveryGatekeeperFixture(tokens int) (*TokenGatekeeper, *int) {
	summarizeCalls := 0
	mockSum := &agenttest.MockSummarizer{}
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		summarizeCalls++
		return "summary", nil, nil
	})
	estimator := NewStrategy(&agenttest.MockTokenCounter{Tokens: tokens})
	tg := newTokenGatekeeper(estimator, mockSum, withMaxTokens(20000))
	return tg, &summarizeCalls
}

// TestTokenGatekeeper_RecoveryFromOverflow_ForcesSummarization verifies
// ADR-059 Lever 1: recovery forces summarization even when the estimate is
// UNDER the 90% pressure threshold, while the normal path stays byte-identical
// (no summarizer call, window unchanged). Fixture: 12 turns / 24 msgs,
// Tokens 12000 < 18000 = 90% of MaxTokens 20000.
func TestTokenGatekeeper_RecoveryFromOverflow_ForcesSummarization(t *testing.T) {
	tests := []struct {
		name                       string
		recovery                   bool
		wantSummarizeCalls         int
		wantSummarizationAttempted bool
		wantSummarizedTurns        int
		wantHistoryLen             int
	}{
		{
			name:                       "recovery_forces_summarization_under_threshold",
			recovery:                   true,
			wantSummarizeCalls:         1,
			wantSummarizationAttempted: true,
			wantSummarizedTurns:        6,
			wantHistoryLen:             14,
		},
		{
			name:                       "normal_path_unaffected",
			recovery:                   false,
			wantSummarizeCalls:         0,
			wantSummarizationAttempted: false,
			wantSummarizedTurns:        0,
			wantHistoryLen:             24,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg, summarizeCalls := recoveryGatekeeperFixture(12000)
			req := &ContextRequest{
				History:              makeUnpinnedTurns(12),
				RecoveryFromOverflow: tt.recovery,
			}

			err := tg.Transform(context.Background(), req)

			require.NoError(t, err)
			require.Equal(t, tt.wantSummarizeCalls, *summarizeCalls)
			require.Equal(t, tt.wantSummarizationAttempted, req.Metadata.SummarizationAttempted)
			require.Equal(t, tt.wantSummarizedTurns, req.Metadata.SummarizedTurns)
			require.Len(t, req.History, tt.wantHistoryLen)
		})
	}
}

// TestTokenGatekeeper_RecoveryFromOverflow_TooShortHistoryDegradesGracefully
// verifies the inherited guard: recovery with fewer than 10 messages forces
// the summarization attempt, which degrades to "tokens unchanged" (no error,
// no summarizer call, window untouched). Fixture: 4 turns / 8 msgs.
func TestTokenGatekeeper_RecoveryFromOverflow_TooShortHistoryDegradesGracefully(t *testing.T) {
	tg, summarizeCalls := recoveryGatekeeperFixture(12000)
	req := &ContextRequest{
		History:              makeUnpinnedTurns(4),
		RecoveryFromOverflow: true,
	}

	err := tg.Transform(context.Background(), req)

	require.NoError(t, err)
	require.Equal(t, 0, *summarizeCalls)
	require.True(t, req.Metadata.MaintenanceBlocked)
	require.False(t, req.Metadata.SummarizationAttempted)
	require.Len(t, req.History, 8)
	require.Equal(t, 12000, req.Metadata.FinalTokenCount)
}

// TestTokenGatekeeper_RecoveryFromOverflow_ReducedHardLimitFailsFast verifies
// ADR-059 Lever 2 in the Q4-c zone (Tokens 18500):
//
//	> 18000 (90% pressure threshold) → summarise on the normal path;
//	≤ 19000 (normal hard limit = 20000 − min(2000, 1000)) → normal path passes;
//	> 18000 (recovery hard limit = 20000 − 2×1000) → recovery fails fast with
//	ErrContextLimitExceeded after exactly one summarizer call (the
//	SummarizationAttempted guard prevents double-summarization).
func TestTokenGatekeeper_RecoveryFromOverflow_ReducedHardLimitFailsFast(t *testing.T) {
	tests := []struct {
		name                       string
		recovery                   bool
		wantErr                    bool
		wantSummarizeCalls         int
		wantSummarizationAttempted bool
	}{
		{
			name:                       "normal_path_summarises_and_passes",
			recovery:                   false,
			wantErr:                    false,
			wantSummarizeCalls:         1,
			wantSummarizationAttempted: true,
		},
		{
			name:                       "recovery_fails_fast_at_reduced_ceiling",
			recovery:                   true,
			wantErr:                    true,
			wantSummarizeCalls:         1,
			wantSummarizationAttempted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg, summarizeCalls := recoveryGatekeeperFixture(18500)
			req := &ContextRequest{
				History:              makeUnpinnedTurns(12),
				RecoveryFromOverflow: tt.recovery,
			}

			err := tg.Transform(context.Background(), req)

			if tt.wantErr {
				require.ErrorIs(t, err, llm.ErrContextLimitExceeded)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantSummarizeCalls, *summarizeCalls)
			require.Equal(t, tt.wantSummarizationAttempted, req.Metadata.SummarizationAttempted)
		})
	}
}
