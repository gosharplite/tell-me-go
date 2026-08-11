// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/require"
)

// recoveryTestHistory builds n unpinned user/model turns for the ADR-059
// recovery-signal wiring tests.
func recoveryTestHistory(n int) []*llm.Content {
	var contents []*llm.Content
	for i := 0; i < n; i++ {
		contents = append(contents,
			&llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("u%d", i+1)}}},
			&llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("m%d", i+1)}}},
		)
	}
	return contents
}

// TestRecoveryStep_ContextOverflow_SetsRecoveryFromOverflow verifies D1:
// on the first provider context overflow (RetryCount == 0) RecoveryStep raises
// the ADR-059 recovery signal before re-entering PhaseRefining.
func TestRecoveryStep_ContextOverflow_SetsRecoveryFromOverflow(t *testing.T) {
	turn := &Turn{
		State: &TurnState{
			Phase:      PhaseRecovering,
			LastError:  llm.ErrContextLimitExceeded,
			RetryCount: 0,
		},
		Clock: &agenttest.MockClock{},
	}
	step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3, Backoff: time.Millisecond}}

	res, err := step.Process(context.Background(), turn)

	require.NoError(t, err)
	require.Equal(t, PhaseRefining, res.NextPhase)
	require.True(t, turn.State.RecoveryFromOverflow, "first overflow must raise the ADR-059 recovery signal")
	require.Equal(t, 1, turn.State.RetryCount)
}

// TestContextRefiner_PassesRecoverySignalToPrepare verifies the wiring
// end-to-end: ContextRefiner reads Turn.State.RecoveryFromOverflow and passes
// WithOverflowRecovery() into Prepare, so the gatekeeper forces summarization.
// FACTORY MUST BE NON-NIL — a nil factory means no pipeline/gatekeeper and the
// test would fail for the wrong reason.
func TestContextRefiner_PassesRecoverySignalToPrepare(t *testing.T) {
	// buildCM returns a fresh Manager with a counting summarizer; each subtest
	// gets its own fixture so the call counters assert per-scenario.
	buildCM := func() (*sessctx.Manager, *int) {
		counter := &agenttest.MockTokenCounter{Tokens: 12000}
		strategy := sessctx.NewStrategy(counter)

		summarizeCalls := 0
		mockSum := &agenttest.MockSummarizer{}
		mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			summarizeCalls++
			return "summary", nil, nil
		})

		strategy.SetLimits(20000, 100, 200)
		factory := &sessctx.Factory{Estimator: strategy, Summarizer: mockSum}

		history := &agenttest.MockHistoryManager{}
		history.SetInternalContents(recoveryTestHistory(12))

		cm := sessctx.NewManager(strategy, history, nil, factory)
		return cm, &summarizeCalls
	}

	t.Run("signal_flows_into_prepare", func(t *testing.T) {
		cm, summarizeCalls := buildCM()
		turn := &Turn{
			Index:        0,
			CtxManager:   cm,
			TokenCounter: &agenttest.MockTokenCounter{Tokens: 12000},
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{RecoveryFromOverflow: true},
		}

		res, err := (&ContextRefiner{}).Process(context.Background(), turn)

		require.NoError(t, err)
		require.Equal(t, PhaseInference, res.NextPhase)
		require.Equal(t, 1, *summarizeCalls, "recovery signal must force summarization through Prepare")
		require.NotNil(t, turn.State.Metadata)
		require.True(t, turn.State.Metadata.SummarizationAttempted)
		require.NotNil(t, turn.State.PreparedHistory)
	})

	t.Run("no_signal_no_forced_summarization", func(t *testing.T) {
		cm, summarizeCalls := buildCM()
		turn := &Turn{
			Index:        0,
			CtxManager:   cm,
			TokenCounter: &agenttest.MockTokenCounter{Tokens: 12000},
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		res, err := (&ContextRefiner{}).Process(context.Background(), turn)

		require.NoError(t, err)
		require.Equal(t, PhaseInference, res.NextPhase)
		require.Equal(t, 0, *summarizeCalls, "no recovery signal must not force summarization")
		require.NotNil(t, turn.State.Metadata)
		require.False(t, turn.State.Metadata.SummarizationAttempted)
	})
}
