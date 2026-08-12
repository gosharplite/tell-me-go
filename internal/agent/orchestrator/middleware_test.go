// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestWithStatusReporter_Scenarios(t *testing.T) {
	t.Run("Scenario A: Inference Header", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		engine := &Engine{events: bus}
		mw := engine.withStatusReporter()

		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			return ProcessResult{NextPhase: PhaseComplete}, nil
		})

		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
		turn := &Turn{
			State:      &TurnState{Phase: PhaseInference, RetryCount: 0},
			CtxManager: cm,
			Clock:      &agenttest.MockClock{},
		}

		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)

		// Should have published TurnStatusEvent (Header)
		found := false
		for _, e := range bus.GetEvents() {
			if evt, ok := e.(events.TurnStatusEvent); ok {
				assert.False(t, evt.Status.IsPostCall)
				assert.False(t, evt.Status.IsFinal)
				found = true
			}
		}
		assert.True(t, found, "TurnStatusEvent (header) should be published")
	})

	t.Run("Scenario B: Persisting Footer and Metrics", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		engine := &Engine{events: bus}
		mw := engine.withStatusReporter()

		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			return ProcessResult{NextPhase: PhaseComplete}, nil
		})

		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
		turn := &Turn{
			State: &TurnState{
				Phase:   PhasePersisting,
				Metrics: &llm.Metrics{PromptTokens: 10},
			},
			CtxManager: cm,
			Clock:      &agenttest.MockClock{},
		}

		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)

		// Should have published two events: Metrics line and Ready footer
		var postCallCount, finalCount int
		for _, e := range bus.GetEvents() {
			if evt, ok := e.(events.TurnStatusEvent); ok {
				if evt.Status.IsPostCall {
					postCallCount++
				}
				if evt.Status.IsFinal {
					finalCount++
				}
			}
		}
		assert.Equal(t, 1, postCallCount, "Should publish one post-call status event (metrics)")
		assert.Equal(t, 1, finalCount, "Should publish one final status event (ready footer)")
	})

	t.Run("Scenario C: Error handling", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		engine := &Engine{events: bus}
		mw := engine.withStatusReporter()

		expectedErr := errors.New("processor failed")
		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			return ProcessResult{}, expectedErr
		})

		hMock := &agenttest.MockHistoryManager{}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
		turn := &Turn{
			State:      &TurnState{Phase: PhaseInference},
			CtxManager: cm,
			Clock:      &agenttest.MockClock{},
		}

		_, err := mw(next).Process(context.Background(), turn)
		assert.ErrorIs(t, err, expectedErr)
	})
}

func TestWithMetrics_Scenarios(t *testing.T) {
	t.Run("Scenario A: Processor returns metrics", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		engine := &Engine{events: bus}
		mw := engine.withMetrics()

		metrics := &llm.Metrics{PromptTokens: 100, ResponseTokens: 50}
		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			turn.State.Metrics = metrics
			return ProcessResult{}, nil
		})

		startTime := time.Now()
		turn := &Turn{
			State:     &TurnState{Phase: PhaseInference},
			StartTime: startTime,
		}

		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)

		found := false
		for _, e := range bus.GetEvents() {
			if evt, ok := e.(events.UsageMetricsEvent); ok {
				assert.Equal(t, metrics, evt.Metrics)
				assert.Equal(t, startTime, evt.StartTime)
				found = true
			}
		}
		assert.True(t, found, "UsageMetricsEvent should be published")
	})

	t.Run("Scenario B: Processor returns nil metrics", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		engine := &Engine{events: bus}
		mw := engine.withMetrics()

		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			turn.State.Metrics = nil
			return ProcessResult{}, nil
		})

		turn := &Turn{
			State: &TurnState{Phase: PhaseInference},
		}

		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)

		for _, e := range bus.GetEvents() {
			_, ok := e.(events.UsageMetricsEvent)
			assert.False(t, ok, "UsageMetricsEvent should NOT be published for nil metrics")
		}
	})

	t.Run("Scenario C: Cost accumulation", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		tracker := &agenttest.MockCostTracker{}
		engine := &Engine{events: bus}
		mw := engine.withMetrics()

		metrics := &llm.Metrics{PromptTokens: 100}
		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			turn.State.Metrics = metrics
			return ProcessResult{}, nil
		})

		turn := &Turn{
			State:       &TurnState{Phase: PhaseInference},
			CostTracker: tracker,
		}

		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)
		assert.Equal(t, 0.05, turn.State.TaskCost)
		assert.Equal(t, 0.05, turn.State.Metrics.Cost)
	})

	t.Run("Scenario D: Run-scoped task cost via LoopDetector", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		tracker := &agenttest.MockCostTracker{}
		engine := &Engine{events: bus}
		mw := engine.withMetrics()

		metrics := &llm.Metrics{PromptTokens: 100}
		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			turn.State.Metrics = metrics
			return ProcessResult{}, nil
		})

		turn := &Turn{
			State:        &TurnState{Phase: PhaseInference},
			CostTracker:  tracker,
			LoopDetector: newLoopDetector(),
		}

		// Run the middleware twice with the same turn and the same detector:
		// the Run-scoped cumulative task cost accumulates on the detector
		// (d.taskCost += turnCost) across calls, and Turn.State.TaskCost
		// mirrors the detector value.
		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)
		_, err = mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)

		// d.taskCost += turnCost accumulates across calls (0.05 + 0.05).
		assert.Equal(t, 0.10, turn.LoopDetector.taskCost)
		// Turn.State.TaskCost = d.taskCost mirrors the detector, not a separate counter.
		assert.Equal(t, turn.LoopDetector.taskCost, turn.State.TaskCost)
		assert.Equal(t, 0.10, turn.State.TaskCost)
		// Metrics.Cost is per-call, not cumulative.
		assert.Equal(t, 0.05, turn.State.Metrics.Cost)

		// Block-level side effect still fires on this path: one
		// UsageMetricsEvent per inference call (two calls → two events).
		usageCount := 0
		for _, e := range bus.GetEvents() {
			if _, ok := e.(events.UsageMetricsEvent); ok {
				usageCount++
			}
		}
		assert.Equal(t, 2, usageCount, "UsageMetricsEvent should be published on every inference call")
	})
}

func TestLoopDetector_Scenarios(t *testing.T) {
	t.Run("Detect Text Loop", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		e := &Engine{loopDetector: newLoopDetector()}
		mw := e.withLoopDetector()

		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			turn.State.Response = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Repeat me"}}}
			return ProcessResult{}, nil
		})

		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial"}}}}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

		turn := &Turn{
			State: &TurnState{
				Phase: PhaseInference,
			},
			CtxManager: cm,
			Events:     bus,
		}

		// First call - no loop
		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)
		assert.NotNil(t, turn.State.Response, "Response should NOT be nil on first call")
		assert.Equal(t, 1, len(e.loopDetector.recentResponseHashes))

		// Second call with same response - loop detected (triggers on immediate duplicate for text)
		_, err = mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)
		assert.Nil(t, turn.State.Response, "Response should be cleared on loop detection (2nd call)")

		found := false
		for _, e := range bus.GetEvents() {
			if evt, ok := e.(events.SystemMessageEvent); ok {
				if evt.Level == "warn" && evt.Message == "Infinite loop detected! Injecting corrective feedback to break the cycle..." {
					found = true
				}
			}
		}
		assert.True(t, found, "SystemMessageEvent should be published")
	})

	t.Run("Detect Tool Loop", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		e := &Engine{loopDetector: newLoopDetector()}
		mw := e.withLoopDetector()

		next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
			turn.State.Response = &llm.Content{
				Role: "model",
				Parts: []*llm.Part{{
					FunctionCall: &llm.FunctionCall{
						Name: "test_tool",
						Args: map[string]interface{}{"cmd": "ls"},
					},
				}},
			}
			return ProcessResult{}, nil
		})

		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial"}}}}
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

		// Seed the detector with different response hashes to prevent early
		// text-loop detection; the tool-call map is pre-allocated by newLoopDetector.
		e.loopDetector.recentResponseHashes = []string{"h1", "h2", "h3", "h4", "h5"}
		e.loopDetector.toolCallCount = make(map[string]int)

		turn := &Turn{
			State: &TurnState{
				Phase: PhaseInference,
			},
			CtxManager: cm,
			Events:     bus,
		}

		// Repeat 5 times (limit is 5)
		for i := 0; i < 5; i++ {
			// Change the hash to avoid text loop detection on subsequent calls
			// Note: mw(next) will calculate hash of current response and add it.
			// To bypass, we can just ensure the hash of the current response is NOT
			// in the detector's recentResponseHashes yet.
			_, _ = mw(next).Process(context.Background(), turn)
			assert.NotNil(t, turn.State.Response, "Should not be nil on attempt %d", i+1)

			// Manually replace the detector's response-hash window to keep
			// bypassing text loop detection
			e.loopDetector.recentResponseHashes = []string{"unique" + time.Now().String() + string(rune(i))}
		}

		// 6th call - tool loop detected
		_, _ = mw(next).Process(context.Background(), turn)
		assert.Nil(t, turn.State.Response, "Should be cleared on 6th call due to tool loop")
	})
}

func TestPublishTurnStatus_EventBusError(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	bus.SetPublishErr(errors.New("bus failure"))

	engine := &Engine{events: bus}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
	turn := &Turn{
		State:      &TurnState{},
		CtxManager: cm,
		Clock:      &agenttest.MockClock{},
	}

	// This should not panic and should log the error
	engine.publishTurnStatus(context.Background(), turn, false, false)
}

func TestWithMetrics_EventBusError(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	bus.SetPublishErr(errors.New("bus failure"))

	engine := &Engine{events: bus}
	mw := engine.withMetrics()

	metrics := &llm.Metrics{PromptTokens: 100}
	next := TurnProcessorFunc(func(ctx context.Context, turn *Turn) (ProcessResult, error) {
		turn.State.Metrics = metrics
		return ProcessResult{}, nil
	})

	turn := &Turn{
		State: &TurnState{Phase: PhaseInference},
	}

	// This should not panic
	_, err := mw(next).Process(context.Background(), turn)
	assert.NoError(t, err)
}

func TestHandleLoopBreak_Error_Internal(t *testing.T) {
	bus := &eventstest.MockEventBus{}

	hMock := &agenttest.MockHistoryManager{}
	hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial"}}}}
	hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
		if content.Role == "model" {
			return errors.New("history persistence failed")
		}
		return nil
	}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

	turn := &Turn{
		State: &TurnState{
			Response: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "loop"}}},
		},
		CtxManager: cm,
		Events:     bus,
	}

	_, err := handleLoopBreak(context.Background(), turn)
	assert.Error(t, err)
	assert.Equal(t, "history persistence failed", err.Error())
}

func TestTruncateSafe_Middleware(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello world", 5, "hello..."},
		{"hello", 10, "hello"},
		{"こんにちは", 2, "こん..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			res := TruncateSafe([]byte(tt.input), tt.max)
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestPublishTurnStatus_NoCostTracker(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	engine := &Engine{events: bus}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

	turn := &Turn{
		State:       &TurnState{},
		CtxManager:  cm,
		Clock:       &agenttest.MockClock{},
		CostTracker: nil,
	}

	engine.publishTurnStatus(context.Background(), turn, false, false)

	found := false
	for _, e := range bus.GetEvents() {
		if _, ok := e.(events.TurnStatusEvent); ok {
			found = true
		}
	}
	assert.True(t, found)
}

func TestHandleLoopBreak_Error_Warning(t *testing.T) {
	bus := &eventstest.MockEventBus{}

	hMock := &agenttest.MockHistoryManager{}
	// Seed history
	hMock.SetInternalContents([]*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "initial"}}}})

	callCount := 0
	hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
		callCount++
		if callCount == 2 { // Fail on the second call (the warning message)
			return errors.New("warning persistence failed")
		}
		// Update contents to allow next AddContent to call AddContentFunc (alternating roles)
		hMock.Mu.Lock()
		hMock.Contents = append(hMock.Contents, content)
		hMock.Mu.Unlock()
		return nil
	}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

	turn := &Turn{
		State: &TurnState{
			Response: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "loop"}}},
		},
		CtxManager: cm,
		Events:     bus,
	}

	_, err := handleLoopBreak(context.Background(), turn)
	assert.Error(t, err)
	assert.Equal(t, "warning persistence failed", err.Error())
}

func TestPublishTurnStatus_ErrBusNotInitialized(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	bus.SetPublishErr(events.ErrBusNotInitialized)

	engine := &Engine{events: bus}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
	turn := &Turn{
		State:      &TurnState{},
		CtxManager: cm,
		Clock:      &agenttest.MockClock{},
	}

	// This should not panic and should NOT log the error
	engine.publishTurnStatus(context.Background(), turn, false, false)
}

func TestPublishTurnStatus_ContextCancelled(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	engine := &Engine{events: bus}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
	turn := &Turn{
		State:      &TurnState{},
		CtxManager: cm,
		Clock:      &agenttest.MockClock{},
	}

	// Create a pre-cancelled context to trigger the timeout/short-circuit
	// inside SafePublish. This exercises the path where bus.Publish is never
	// called because context check fails first.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Must not panic. SafePublish will return a wrapped context.Canceled error,
	// which is NOT ErrBusNotInitialized, so the error-logging branch fires.
	assert.NotPanics(t, func() {
		engine.publishTurnStatus(ctx, turn, false, false)
	})

	// Verify no event was published (context was cancelled before Publish ran)
	assert.NotEmpty(t, bus.GetEvents(), "events should be published despite cancelled caller context — SafePublish uses context.Background()")
}
func TestPublishTurnStatus_ContextDeadlineExceeded(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	engine := &Engine{events: bus}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)
	turn := &Turn{
		State:      &TurnState{},
		CtxManager: cm,
		Clock:      &agenttest.MockClock{},
	}

	// Use a deadline already in the past so the context is immediately expired.
	// SafePublish wraps this in its own WithTimeout(2s), but the parent context
	// deadline takes precedence.
	deadline := time.Now().Add(-1 * time.Second) // 1 second in the past
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	// No sleep needed — deadline is already expired

	// Must not panic. The deadline triggers a context.DeadlineExceeded error
	// wrapped by SafePublish, which is NOT ErrBusNotInitialized, so the
	// error-logging branch fires.
	assert.NotPanics(t, func() {
		engine.publishTurnStatus(ctx, turn, false, false)
	})

	// Verify no event was published (deadline expired before Publish ran)
	assert.NotEmpty(t, bus.GetEvents(), "events should be published despite expired caller deadline — SafePublish uses context.Background()")
}
func TestPublishTurnStatus_WithCostTracker(t *testing.T) {
	bus := &eventstest.MockEventBus{}
	engine := &Engine{events: bus}

	hMock := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

	tracker := &agenttest.MockCostTracker{}
	turn := &Turn{
		State:       &TurnState{},
		CtxManager:  cm,
		Clock:       &agenttest.MockClock{},
		CostTracker: tracker, // ← triggers the CostTracker != nil branch
	}

	// Must not panic. The CostTracker branch will execute, calling
	// GetStats on the mock.
	assert.NotPanics(t, func() {
		engine.publishTurnStatus(context.Background(), turn, false, false)
	})

	// Verify the event was published successfully (happy path)
	found := false
	for _, e := range bus.GetEvents() {
		if evt, ok := e.(events.TurnStatusEvent); ok {
			// MockCostTracker returns GetStats → (UsageStats{}, 0.05)
			// — UsageStats fields are all zero.
			assert.Equal(t, 0.05, evt.Status.SessionCost)
			assert.Equal(t, int64(0), evt.Status.TotalM)
			assert.Equal(t, int64(0), evt.Status.TotalH)
			assert.Equal(t, int64(0), evt.Status.TotalO)
			found = true
		}
	}
	assert.True(t, found, "TurnStatusEvent should be published")
}

func TestHandleLoopBreak_SyntheticToolResults(t *testing.T) {
	// Case 1: Response WITH tool calls → response persisted + synthetic tool results injected
	t.Run("tool-call response gets synthetic tool results", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}

		hMock := &agenttest.MockHistoryManager{}
		hMock.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "initial"}}},
		})
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

		turn := &Turn{
			State: &TurnState{
				HasToolCalls: true,
				Response: &llm.Content{
					Role: "model",
					Parts: []*llm.Part{{
						FunctionCall: &llm.FunctionCall{
							Name: "replace_text",
							Args: map[string]interface{}{"old": "x", "new": "y"},
						},
					}},
				},
			},
			CtxManager: cm,
			Events:     bus,
		}

		result, err := handleLoopBreak(context.Background(), turn)
		assert.NoError(t, err)
		assert.Equal(t, PhaseComplete, result.NextPhase)

		// Existing behavior preserved
		assert.True(t, turn.State.HasToolCalls, "HasToolCalls should be true after loop break")
		assert.Nil(t, turn.State.Response, "Response should be cleared after loop break")

		// Response IS persisted (unlike before)
		contents := hMock.GetContents()
		assert.Len(t, contents, 3, "history: seed + model response + synthetic tool result")

		// Model response with FunctionCall was persisted
		assert.Equal(t, "model", contents[1].Role)
		assert.NotNil(t, contents[1].Parts[0].FunctionCall)
		assert.Equal(t, "replace_text", contents[1].Parts[0].FunctionCall.Name)

		// Synthetic tool result injected
		assert.Equal(t, "tool", contents[2].Role)
		assert.NotNil(t, contents[2].Parts[0].FunctionResponse)
		assert.Equal(t, "replace_text", contents[2].Parts[0].FunctionResponse.Name)
		assert.Equal(t, LoopWarning, contents[2].Parts[0].FunctionResponse.Response["error"])

		// No user-role warning
		for _, c := range contents {
			if c.Role == "user" {
				for _, p := range c.Parts {
					assert.NotEqual(t, LoopWarning, p.Text, "user warning should not be present")
				}
			}
		}

		// The system warning event was published
		found := false
		for _, e := range bus.GetEvents() {
			if evt, ok := e.(events.SystemMessageEvent); ok {
				if evt.Level == "warn" {
					found = true
				}
			}
		}
		assert.True(t, found, "loop-break warning event should be published")
	})

	// Case 2: Response WITHOUT tool calls → persisted + user warning (existing behavior preserved)
	t.Run("text-only response persisted", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}

		hMock := &agenttest.MockHistoryManager{}
		hMock.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "initial"}}},
		})
		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

		turn := &Turn{
			State: &TurnState{
				HasToolCalls: false,
				Response:     &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "I'm stuck"}}},
			},
			CtxManager: cm,
			Events:     bus,
		}

		result, err := handleLoopBreak(context.Background(), turn)
		assert.NoError(t, err)
		assert.Equal(t, PhaseComplete, result.NextPhase)

		// Existing behavior preserved
		assert.True(t, turn.State.HasToolCalls, "HasToolCalls should be true after loop break")
		assert.Nil(t, turn.State.Response, "Response should be cleared after loop break")

		// History assertions: seed + model text response + warning = 3
		contents := hMock.GetContents()
		assert.Len(t, contents, 3, "history should contain seed + model response + warning")

		assert.Equal(t, "model", contents[1].Role, "second entry should be the model text response")
		assert.Equal(t, "I'm stuck", contents[1].Parts[0].Text)

		assert.Equal(t, "user", contents[2].Role, "third entry should be the user warning")
		assert.Equal(t, LoopWarning, contents[2].Parts[0].Text)
	})

	// Case 3: Error from synthetic tool AddContent propagates
	t.Run("error from synthetic tool AddContent propagates", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}

		addContentCalls := 0
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "initial"}}},
		})
		hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
			addContentCalls++
			if content.Role == "tool" {
				return assert.AnError // fail on synthetic tool result injection
			}
			// Default append for other roles
			hMock.Mu.Lock()
			hMock.Contents = append(hMock.Contents, llm.CloneContent(content))
			hMock.Mu.Unlock()
			return nil
		}

		cm := sessctx.NewManager(sessctx.NewStrategy(&agenttest.MockTokenCounter{}), hMock, bus, nil)

		turn := &Turn{
			State: &TurnState{
				HasToolCalls: true,
				Response: &llm.Content{
					Role: "model",
					Parts: []*llm.Part{{
						FunctionCall: &llm.FunctionCall{
							Name: "replace_text",
							Args: map[string]interface{}{"old": "x", "new": "y"},
						},
					}},
				},
			},
			CtxManager: cm,
			Events:     bus,
		}

		_, err := handleLoopBreak(context.Background(), turn)
		assert.Error(t, err, "error from synthetic AddContent should propagate")
		assert.Equal(t, 2, addContentCalls, "should attempt: response + synthetic tool")
	})
}
