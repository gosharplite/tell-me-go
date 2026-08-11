// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

// turnCaptureHook captures the *Turn and *TurnState observed at BeforeTurn,
// plus a shallow entry-time snapshot of the TurnState (the live *TurnState is
// mutated during execution, so the pristine-state assertions must read the
// snapshot). Run is synchronous in these tests, so plain slices are safe.
type turnCaptureHook struct {
	turns      []*Turn
	states     []*TurnState
	entryState []TurnState
}

func (h *turnCaptureHook) BeforeTurn(turn *Turn) {
	h.turns = append(h.turns, turn)
	h.states = append(h.states, turn.State)
	h.entryState = append(h.entryState, *turn.State)
}

func (h *turnCaptureHook) AfterTurn(turn *Turn, err error) {}

func (h *turnCaptureHook) OnPhaseTransition(from, to TurnPhase, state *TurnState) {}

// TestRun_FreshTurnPerIteration verifies T3: Run allocates a fresh *Turn per
// iteration (constructor-is-the-reset), keeps StartTime Run-fixed, installs
// the same per-Run loopDetector on every turn, and hands each fresh turn a
// structurally pristine *TurnState at entry.
func TestRun_FreshTurnPerIteration(t *testing.T) {
	// Turn 0 -> FunctionCall (HasToolCalls=true, Run continues to turn 1)
	// Turn 1 -> text response (HasToolCalls=false, Run stops).
	turnCount := 0
	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			turnCount++
			content := &llm.Content{Role: "model"}
			if turnCount == 1 {
				content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
			} else {
				content.Parts = []*llm.Part{{Text: "done"}}
			}
			return content, &llm.Metrics{}, nil
		},
	}

	ex := &agenttest.MockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test"}}}}, nil
		},
	}

	reg := &agenttest.MockToolRegistry{}
	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(reg))
	bus := &eventstest.MockEventBus{}
	hMock := &agenttest.MockHistoryManager{}
	// Seed a user message: the first history entry must be user-role for
	// role-alternation validation in CtxManager.AddContent.
	hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}}}
	cm := sessctx.NewManager(strategy, hMock, bus, nil)
	cm.Pipeline = sessctx.NewContextPipeline()
	strategy.SetLimits(1000, 5, 10)

	hook := &turnCaptureHook{}
	e := NewEngine(gw, ex, cm, reg, bus, strategy,
		WithEngineClock(&agenttest.MockClock{}),
		WithEngineHook(hook),
	)

	startTime := time.Now()
	err := e.Run(context.Background(), startTime, "")
	require.NoError(t, err)

	// Two turns executed.
	require.Len(t, hook.turns, 2)

	// Fresh *Turn per iteration: distinct pointers (Turn and TurnState), index
	// advances, StartTime stays Run-fixed, and the same per-Run detector is
	// installed on both.
	require.NotSame(t, hook.turns[0], hook.turns[1])
	require.NotSame(t, hook.states[0], hook.states[1])
	require.Equal(t, 1, hook.turns[1].Index)
	require.Equal(t, hook.turns[0].StartTime, hook.turns[1].StartTime)
	require.NotNil(t, hook.turns[0].LoopDetector)
	require.Same(t, hook.turns[0].LoopDetector, hook.turns[1].LoopDetector)

	// Turn-2 state is pristine at entry (fresh Turn = structural reset).
	state := hook.entryState[1]
	require.Equal(t, PhaseGuard, state.Phase)
	require.Nil(t, state.Response)
	require.Nil(t, state.ToolResponse)
	require.False(t, state.HasToolCalls)
	require.Nil(t, state.ToolReasons)
	require.Nil(t, state.LastError)
	require.Nil(t, state.PreparedHistory)
	require.False(t, state.RecoveryFromOverflow)
	require.Zero(t, state.RetryCount)
	require.Nil(t, state.Metrics)
	require.Zero(t, state.Tokens)
	require.Zero(t, state.TaskCost)
	require.Nil(t, state.Metadata)
	require.Equal(t, 1, state.CurrentTurns)

	// The per-Run detector's tool-call map is pre-allocated (non-nil).
	require.NotNil(t, hook.turns[1].LoopDetector.toolCallCount)

	// Soft assertion: Run used the per-Run detector, not the Engine fallback —
	// e.loopDetector remains in its newLoopDetector zero state.
	require.Zero(t, e.loopDetector.taskCost)
	require.Empty(t, e.loopDetector.toolCallCount)
	require.Nil(t, e.loopDetector.recentResponseHashes)
	require.False(t, e.loopDetector.seenRateLimit)
}
