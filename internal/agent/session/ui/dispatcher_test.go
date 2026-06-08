// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
)

// --- test doubles ---

// spyRenderer records calls for assertion without depending on testify/mock.
type spyRenderer struct {
	logToolCallCalls             []logToolCallArgs
	logToolResultCalls           []logToolResultArgs
	startSpinnerWithMetricsCalls int
	startSpinnerWithStatusCalls  int
}

type logToolCallArgs struct {
	calls     []*llm.FunctionCall
	turn      int
	maxTurns  int
	showTools bool
}

type logToolResultArgs struct {
	name      string
	result    tools.ToolResult
	showTools bool
}

func (s *spyRenderer) LogToolCall(_ context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	s.logToolCallCalls = append(s.logToolCallCalls, logToolCallArgs{calls, turn, maxTurns, showTools})
}

func (s *spyRenderer) LogToolResult(_ context.Context, name string, result tools.ToolResult, showTools bool) {
	s.logToolResultCalls = append(s.logToolResultCalls, logToolResultArgs{name, result, showTools})
}

// Remaining ports.UIRenderer methods — no-op stubs
func (s *spyRenderer) StartSpinner(_ context.Context) func() { return func() {} }
func (s *spyRenderer) StartSpinnerWithStatus(_ context.Context, _ string) func() {
	s.startSpinnerWithStatusCalls++
	return func() {}
}
func (s *spyRenderer) StartSpinnerWithMetrics(_ context.Context, _ string) func() {
	s.startSpinnerWithMetricsCalls++
	return func() {}
}
func (s *spyRenderer) RenderResponse(_ context.Context, _ *llm.Content, _, _ bool) {}
func (s *spyRenderer) LogTurnStatus(_ context.Context, _ events.TurnStatus)        {}
func (s *spyRenderer) LogUsage(_ context.Context, _ *llm.Metrics, _ string, _ time.Time) {
}
func (s *spyRenderer) LogSystemMessage(_ context.Context, _ string, _ string)      {}
func (s *spyRenderer) RenderHealthReport(_ context.Context, _ *ports.HealthReport) {}
func (s *spyRenderer) SetUseColor(_ bool)                                          {}
func (s *spyRenderer) SetForceSpinner(_ bool)                                      {}
func (s *spyRenderer) IsTerminalContext() bool                                     { return false }

// --- helper ---

func newTestDispatcher(t *testing.T) (*eventDispatcher, *spyRenderer, *testfixtures.SpyLogger) {
	t.Helper()
	renderer := &spyRenderer{}
	logger := &testfixtures.SpyLogger{}
	sc := newSpinnerCoord(renderer, logger)
	sm := newUIStateMachine(sc)
	d := newEventDispatcher(renderer, logger, sm, sc, false, true, false, "")
	return d, renderer, logger
}

// --- tests ---

func TestHandleToolEvents_ToolCallEvent(t *testing.T) {
	t.Parallel()

	d, renderer, _ := newTestDispatcher(t)
	calls := []*llm.FunctionCall{
		{Name: "search", Args: map[string]interface{}{"query": "test"}},
	}

	d.dispatch(context.Background(), events.ToolCallEvent{
		Calls:    calls,
		Turn:     2,
		MaxTurns: 5,
	})

	assert.Len(t, renderer.logToolCallCalls, 1, "expected one LogToolCall")
	assert.Equal(t, calls, renderer.logToolCallCalls[0].calls)
	assert.Equal(t, 2, renderer.logToolCallCalls[0].turn)
	assert.Equal(t, 5, renderer.logToolCallCalls[0].maxTurns)
	assert.True(t, renderer.logToolCallCalls[0].showTools)
}

func TestHandleToolEvents_ToolResultEvent(t *testing.T) {
	t.Parallel()

	d, renderer, _ := newTestDispatcher(t)
	result := tools.ToolResult{Text: "result content"}

	d.dispatch(context.Background(), events.ToolResultEvent{
		Name:   "search",
		Result: result,
	})

	assert.Len(t, renderer.logToolResultCalls, 1, "expected one LogToolResult")
	assert.Equal(t, "search", renderer.logToolResultCalls[0].name)
	assert.Equal(t, result, renderer.logToolResultCalls[0].result)
	assert.True(t, renderer.logToolResultCalls[0].showTools)
}

func TestHandleToolEvents_NilCallsGuard(t *testing.T) {
	t.Parallel()

	d, renderer, logger := newTestDispatcher(t)

	d.dispatch(context.Background(), events.ToolCallEvent{
		Calls:    nil,
		Turn:     1,
		MaxTurns: 3,
	})

	assert.Empty(t, renderer.logToolCallCalls, "LogToolCall must not be called when Calls is nil")
	assert.Contains(t, logger.GetDebugs(), "handleToolEvents: ToolCallEvent missing Calls")
}

func TestHandleToolEvents_EmptyNameGuard(t *testing.T) {
	t.Parallel()

	d, renderer, logger := newTestDispatcher(t)

	d.dispatch(context.Background(), events.ToolResultEvent{
		Name:   "",
		Result: tools.ToolResult{},
	})

	assert.Empty(t, renderer.logToolResultCalls, "LogToolResult must not be called when Name is empty")
	assert.Contains(t, logger.GetDebugs(), "handleToolEvents: ToolResultEvent missing Name")
}

func TestHandleToolEvents_ContextTODO(t *testing.T) {
	t.Parallel()

	d, renderer, _ := newTestDispatcher(t)
	calls := []*llm.FunctionCall{{Name: "search"}}

	// dispatch with context.TODO() — must not panic (SA1012-compliant)
	d.dispatch(context.TODO(), events.ToolCallEvent{
		Calls:    calls,
		Turn:     1,
		MaxTurns: 3,
	})

	assert.Len(t, renderer.logToolCallCalls, 1, "LogToolCall should still be called with context.TODO()")
}

func TestHandleToolEvents_UnexpectedType(t *testing.T) {
	t.Parallel()

	d, renderer, logger := newTestDispatcher(t)

	// Invoke handleToolEvents directly with a non-tool event type to
	// exercise the default case (the dispatch map would route this
	// correctly in production; the default case is a safety net).
	d.handleToolEvents(context.Background(), events.TurnStarted{Turn: 1, MaxTurns: 3})

	assert.Empty(t, renderer.logToolCallCalls)
	assert.Empty(t, renderer.logToolResultCalls)
	assert.Contains(t, logger.GetDebugs(), "handleToolEvents: unexpected event type")
}

func TestHandleToolEvents_ResumesActiveSpinner(t *testing.T) {
	t.Parallel()

	d, renderer, _ := newTestDispatcher(t)

	// Set an active phase so resumeActiveSpinner returns true.
	d.spinner.activePhase = events.InferenceStartedEvent{Model: "gpt-4"}

	calls := []*llm.FunctionCall{{Name: "search"}}
	d.dispatch(context.Background(), events.ToolCallEvent{
		Calls:    calls,
		Turn:     1,
		MaxTurns: 3,
	})

	assert.Len(t, renderer.logToolCallCalls, 1)
	// Verify stateMachine was set to stateThinking (line 143 reached).
	assert.Equal(t, stateThinking, d.stateMachine.current())
}

func TestEnsureContext(t *testing.T) {
	t.Parallel()

	d, _, _ := newTestDispatcher(t)

	t.Run("nil context falls back to Background", func(t *testing.T) {
		//nolint:staticcheck // intentional nil: exercising ensureContext's nil-context guard
		ctx := d.ensureContext(nil, "testOp")
		if ctx == nil {
			t.Error("expected non-nil context from ensureContext(nil)")
		}
		if ctx != context.Background() {
			t.Error("expected context.Background() from ensureContext(nil)")
		}
	})

	t.Run("non-nil context passed through", func(t *testing.T) {
		type ctxKey struct{}
		incoming := context.WithValue(context.Background(), ctxKey{}, "value")
		ctx := d.ensureContext(incoming, "testOp")
		if ctx != incoming {
			t.Error("expected same context to be returned for non-nil input")
		}
	})

	t.Run("context.TODO passes through unchanged", func(t *testing.T) {
		ctx := d.ensureContext(context.TODO(), "testOp")
		if ctx != context.TODO() {
			t.Error("expected context.TODO() to pass through unchanged")
		}
	})
}

func TestStartSpinnerForPhase_WithMetrics(t *testing.T) {
	t.Parallel()

	renderer := &spyRenderer{}
	logger := &testfixtures.SpyLogger{}
	sc := newSpinnerCoord(renderer, logger)

	// ToolExecutionStartedEvent has withMetrics: true in getSpinnerInfo.
	e := events.ToolExecutionStartedEvent{ToolNames: []string{"test"}}

	started := sc.startSpinnerForPhase(context.Background(), e, stateIdle, nil)

	assert.True(t, started, "expected spinner to be started")
	assert.Equal(t, 1, renderer.startSpinnerWithMetricsCalls, "StartSpinnerWithMetrics should be called once")
	assert.Equal(t, 0, renderer.startSpinnerWithStatusCalls, "StartSpinnerWithStatus should not be called")
}

func TestStartSpinnerForPhase_ResetRendering(t *testing.T) {
	t.Parallel()

	renderer := &spyRenderer{}
	logger := &testfixtures.SpyLogger{}
	sc := newSpinnerCoord(renderer, logger)

	// SummarizationStartedEvent has resetRendering: true, withMetrics: false.
	e := events.SummarizationStartedEvent{}

	var callbackCalled bool
	resetFn := func() uiState {
		callbackCalled = true
		return stateIdle
	}

	started := sc.startSpinnerForPhase(context.Background(), e, stateRendering, resetFn)

	assert.True(t, started, "expected spinner to be started after rendering reset")
	assert.True(t, callbackCalled, "expected resetRendering callback to be invoked")
	assert.Equal(t, 1, renderer.startSpinnerWithStatusCalls, "StartSpinnerWithStatus should be called once")
	assert.Equal(t, 0, renderer.startSpinnerWithMetricsCalls, "StartSpinnerWithMetrics should not be called")
}

func TestHandleUsageMetrics_ResumesSpinner(t *testing.T) {
	t.Parallel()

	d, _, _ := newTestDispatcher(t)

	// Set an active phase so resumeActiveSpinner returns true.
	d.spinner.activePhase = events.InferenceStartedEvent{Model: "gpt-4"}

	d.dispatch(context.Background(), events.UsageMetricsEvent{
		Metrics:   &llm.Metrics{PromptTokens: 10},
		StartTime: time.Now(),
		Context:   context.Background(),
	})

	assert.Equal(t, stateThinking, d.stateMachine.current())
}

func TestHandleToolEvents_ToolResult_ResumesSpinner(t *testing.T) {
	t.Parallel()

	d, _, _ := newTestDispatcher(t)

	// Set an active phase so resumeActiveSpinner returns true.
	d.spinner.activePhase = events.InferenceStartedEvent{}

	d.dispatch(context.Background(), events.ToolResultEvent{
		Name:   "search",
		Result: tools.ToolResult{Text: "ok"},
	})

	assert.Equal(t, stateThinking, d.stateMachine.current())
}

func TestHandleSystemMessage_StatusUpdate_ResumesSpinner(t *testing.T) {
	t.Parallel()

	d, _, _ := newTestDispatcher(t)

	// Set an active phase so resumeActiveSpinner returns true.
	d.spinner.activePhase = events.InferenceStartedEvent{}

	d.dispatch(context.Background(), events.StatusUpdate{
		Message: "msg",
		Level:   "info",
	})

	assert.Equal(t, stateThinking, d.stateMachine.current())
}

func TestHandleSystemMessage_SystemMessageEvent_ResumesSpinner(t *testing.T) {
	t.Parallel()

	d, _, _ := newTestDispatcher(t)

	// Set an active phase so resumeActiveSpinner returns true.
	d.spinner.activePhase = events.InferenceStartedEvent{}

	d.dispatch(context.Background(), events.SystemMessageEvent{
		Message: "msg",
		Level:   "info",
	})

	assert.Equal(t, stateThinking, d.stateMachine.current())
}

func TestHandleSystemMessage_DefaultCase(t *testing.T) {
	t.Parallel()

	d, renderer, _ := newTestDispatcher(t)

	// TurnStarted is neither SystemMessageEvent nor StatusUpdate,
	// so handleSystemMessage hits the default: return at dispatcher.go:173-174.
	d.handleSystemMessage(context.Background(), events.TurnStarted{})

	// Verify no renderer methods were called — the default case returns silently.
	assert.Empty(t, renderer.logToolCallCalls)
	assert.Empty(t, renderer.logToolResultCalls)
	assert.Equal(t, 0, renderer.startSpinnerWithMetricsCalls)
	assert.Equal(t, 0, renderer.startSpinnerWithStatusCalls)
}

func TestStartSpinnerForPhase_UnknownEvent(t *testing.T) {
	t.Parallel()

	renderer := &spyRenderer{}
	logger := &testfixtures.SpyLogger{}
	sc := newSpinnerCoord(renderer, logger)

	// TurnStarted is not a spinner event → getSpinnerInfo returns spinnerInfo{}, false.
	started := sc.startSpinnerForPhase(context.Background(), events.TurnStarted{}, stateIdle, nil)

	assert.False(t, started, "expected spinner not to be started for unknown event")
	assert.Equal(t, 0, renderer.startSpinnerWithMetricsCalls)
	assert.Equal(t, 0, renderer.startSpinnerWithStatusCalls)
}
