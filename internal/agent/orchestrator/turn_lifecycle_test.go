// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// newTurnLifecycleFixture builds the standard T3/T4 fixture: a seeded
// one-user-message history, the default context pipeline, limits
// (1000 tokens, 5 tool turns, 10 history turns), and an Engine wired to the
// given gateway, executor and capture hook with a mocked clock. Run is
// synchronous in these tests, so no mutex is needed. Returns the Engine and
// the mock history manager for post-run inspection.
func newTurnLifecycleFixture(t *testing.T, gw *agenttest.MockGateway, ex *agenttest.MockAgentExecutor, hook *turnCaptureHook) (*Engine, *agenttest.MockHistoryManager) {
	t.Helper()

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

	e := NewEngine(gw, ex, cm, reg, bus, strategy,
		WithEngineClock(&agenttest.MockClock{}),
		WithEngineHook(hook),
	)
	return e, hMock
}

// responseHash reproduces loopDetector.detectLoop's hash of a model response
// (the content with its mutable ID zeroed), so the test can assert that a
// specific response's hash survived the fresh-Turn boundary in
// detector.recentResponseHashes.
func responseHash(t *testing.T, content *llm.Content) string {
	t.Helper()

	sanitized := *content
	sanitized.ID = ""
	raw, err := json.Marshal(&sanitized)
	require.NoError(t, err)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// countLoopWarningsInHistory returns the number of history contents carrying
// the loop-break warning, scanning user-role text messages (text-only loops)
// and tool-role FunctionResponse entries (tool-call loops) — the same dual
// scan as assertLoopWarningInHistory in the integration tests, replicated
// locally because the integration helper lives in another package.
func countLoopWarningsInHistory(t *testing.T, contents []*llm.Content) int {
	t.Helper()

	count := 0
	for _, msg := range contents {
		found := false
		if msg.Role == "user" {
			for _, part := range msg.Parts {
				if part.Text == LoopWarning {
					found = true
				}
			}
		}
		if msg.Role == "tool" {
			for _, part := range msg.Parts {
				if part.FunctionResponse != nil {
					if errStr, ok := part.FunctionResponse.Response["error"].(string); ok && errStr == LoopWarning {
						found = true
					}
				}
			}
		}
		if found {
			count++
		}
	}
	return count
}

// TestRun_FreshTurn_PristineAndAccumulatorsSurvive verifies T4 part (a): the
// fresh Turn per Run iteration leaves turn 2's entry state structurally
// pristine (all ephemeral fields cleared), while the per-Run loopDetector's
// tool-call counter accumulates across the fresh-Turn boundary.
//
// Turn 1 -> 3 identical read_file calls + per-turn text (3 < 5, no loop;
// HasToolCalls=true -> Run continues). Turn 2 -> the same 3 calls again
// (counter reaches 6 > DefaultMaxLoopRepetitions on the 3rd call; the hash
// path does NOT fire first because the per-turn text keeps the response
// hashes distinct — same pattern as the integration test's
// setupTwoTurnRepeatingGateway). Loop break injects synthetic tool feedback
// and continues. Turn 3 -> plain text terminates the Run.
func TestRun_FreshTurn_PristineAndAccumulatorsSurvive(t *testing.T) {
	turnCount := 0
	readFile := func() *llm.Part {
		return &llm.Part{FunctionCall: &llm.FunctionCall{Name: "read_file", Args: map[string]interface{}{"path": "x"}}}
	}
	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			turnCount++
			content := &llm.Content{Role: "model"}
			switch turnCount {
			case 1, 2:
				// 3 identical tool calls per turn (6 total across two turns >
				// DefaultMaxLoopRepetitions of 5). The per-turn text makes each
				// turn's response hash distinct so the tool-call counter path —
				// not the response-hash path — is the trigger on turn 2.
				content.Parts = []*llm.Part{
					readFile(),
					readFile(),
					readFile(),
					{Text: fmt.Sprintf("Turn %d", turnCount)},
				}
			default:
				content.Parts = []*llm.Part{{Text: "final"}}
			}
			return content, &llm.Metrics{}, nil
		},
	}

	ex := &agenttest.MockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "read_file", Response: map[string]interface{}{"result": "ok"}}}}}, nil
		},
	}

	hook := &turnCaptureHook{}
	e, hMock := newTurnLifecycleFixture(t, gw, ex, hook)

	err := e.Run(context.Background(), time.Now(), "")
	require.NoError(t, err)

	// All three turns ran.
	require.Len(t, hook.turns, 3)
	require.Equal(t, 3, turnCount)

	// Pristine turn-2 (Index 1) entry state: the fresh Turn constructor is
	// the reset — every ephemeral field starts zeroed at the guard phase.
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

	// Accumulators survive: the same per-Run detector is installed on every
	// turn of the Run.
	detector := hook.turns[0].LoopDetector
	require.NotNil(t, detector)
	require.Same(t, detector, hook.turns[1].LoopDetector)
	require.Same(t, detector, hook.turns[2].LoopDetector)

	// The tool-call counter survived the fresh-Turn boundary: 3 calls on
	// turn 1 + 3 on turn 2 == 6. The 3rd call of turn 2 crosses
	// DefaultMaxLoopRepetitions (5) and trips detectLoop. Build the expected
	// key exactly the way detectLoop does.
	args, err := json.Marshal(map[string]interface{}{"path": "x"})
	require.NoError(t, err)
	key := "read_file:" + string(args)
	require.Equal(t, 6, detector.toolCallCount[key])

	// The loop break happened on turn 2 (Index 1): handleLoopBreak signals
	// continuation via HasToolCalls=true on that turn's completion, and the
	// final turn ends with HasToolCalls=false.
	require.True(t, hook.states[1].HasToolCalls)
	require.False(t, hook.states[2].HasToolCalls)

	// Exactly one loop-break injection: the synthetic tool-role feedback on
	// turn 2 (one AddContent call carrying 3 FunctionResponse parts).
	require.Equal(t, 1, countLoopWarningsInHistory(t, hMock.GetContents()))
}

// TestRun_FreshTurn_TextLoopAcrossTurns verifies T4 part (b): the per-Run
// loopDetector's response-hash window survives the fresh-Turn boundary — a
// response repeated on a later turn is caught as a hash duplicate.
//
// A pure-text response terminates the Run (shouldStopRunning returns true
// when HasToolCalls is false), so a duplicate of a *pure-text* response can
// never be observed on a later turn: its first occurrence already stops the
// Run. The reachable equivalent carries the repeated text alongside a seed
// tool call so the first occurrence keeps the Run alive:
//
// Turn 1 -> seed tool call (window: [seedHash]). Turn 2 -> text "Repeat" +
// seed tool call (window: [seedHash, repeatHash]). Turn 3 -> the SAME
// text-bearing response again — detectLoop sees repeatHash already in the
// window (it survived two fresh-Turn boundaries) and breaks the loop.
// Turn 4 -> distinct text terminates the Run.
//
// Note: because the duplicated response contains a tool call, the loop-break
// injection is the synthetic tool-role feedback (the user-role text warning
// is unreachable via Run — it requires detectLoop to fire on a response with
// HasToolCalls=false, whose first occurrence always terminates the Run).
func TestRun_FreshTurn_TextLoopAcrossTurns(t *testing.T) {
	turnCount := 0
	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			turnCount++
			content := &llm.Content{Role: "model"}
			switch turnCount {
			case 1:
				// Seed turn: a single tool call keeps the Run alive.
				content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "seed", Args: map[string]interface{}{"a": 1}}}}
			case 2, 3:
				// Same text-bearing response twice: turn 3's inference is a
				// hash duplicate of turn 2's, which the per-Run window must
				// have retained across the fresh-Turn boundary to catch.
				content.Parts = []*llm.Part{
					{Text: "Repeat"},
					{FunctionCall: &llm.FunctionCall{Name: "seed", Args: map[string]interface{}{"a": 1}}},
				}
			default:
				content.Parts = []*llm.Part{{Text: "done"}}
			}
			return content, &llm.Metrics{}, nil
		},
	}

	ex := &agenttest.MockAgentExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "seed", Response: map[string]interface{}{"result": "ok"}}}}}, nil
		},
	}

	hook := &turnCaptureHook{}
	e, hMock := newTurnLifecycleFixture(t, gw, ex, hook)

	err := e.Run(context.Background(), time.Now(), "")
	require.NoError(t, err)

	// All four turns ran.
	require.Len(t, hook.turns, 4)
	require.Equal(t, 4, turnCount)

	// The same per-Run detector is installed on every turn.
	detector := hook.turns[0].LoopDetector
	require.NotNil(t, detector)
	require.Same(t, detector, hook.turns[3].LoopDetector)

	// The response-hash window survived the fresh-Turn boundary: it holds
	// both the turn-1 seed hash and the turn-2 repeat hash (assert the exact
	// hashes by reproducing detectLoop's computation), and has grown to at
	// least 2 entries.
	seedHash := responseHash(t, &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "seed", Args: map[string]interface{}{"a": 1}}}}})
	repeatHash := responseHash(t, &llm.Content{Role: "model", Parts: []*llm.Part{
		{Text: "Repeat"},
		{FunctionCall: &llm.FunctionCall{Name: "seed", Args: map[string]interface{}{"a": 1}}},
	}})
	require.GreaterOrEqual(t, len(detector.recentResponseHashes), 2)
	require.Contains(t, detector.recentResponseHashes, seedHash)
	require.Contains(t, detector.recentResponseHashes, repeatHash)

	// The duplicate was caught on turn 3 (Index 2): handleLoopBreak signals
	// continuation via HasToolCalls=true on that turn's completion, and the
	// final turn ends with HasToolCalls=false.
	require.True(t, hook.states[2].HasToolCalls)
	require.False(t, hook.states[3].HasToolCalls)

	// Exactly one loop-break injection: the synthetic tool-role feedback on
	// turn 3.
	require.Equal(t, 1, countLoopWarningsInHistory(t, hMock.GetContents()))
}
