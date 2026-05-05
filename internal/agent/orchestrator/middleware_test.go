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
}

func TestLoopDetector_Scenarios(t *testing.T) {
	t.Run("Detect Text Loop", func(t *testing.T) {
		bus := &eventstest.MockEventBus{}
		mw := withLoopDetector()

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
				// Ensure clean state
				RecentResponseHashes: nil,
				ToolCallCount:        make(map[string]int),
			},
			CtxManager: cm,
			Events:     bus,
		}

		// First call - no loop
		_, err := mw(next).Process(context.Background(), turn)
		assert.NoError(t, err)
		assert.NotNil(t, turn.State.Response, "Response should NOT be nil on first call")
		assert.Equal(t, 1, len(turn.State.RecentResponseHashes))

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
		mw := withLoopDetector()

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

		turn := &Turn{
			State: &TurnState{
				Phase: PhaseInference,
				// Populate RecentResponseHashes with different hashes to prevent early text-loop detection
				RecentResponseHashes: []string{"h1", "h2", "h3", "h4", "h5"},
				ToolCallCount:        make(map[string]int),
			},
			CtxManager: cm,
			Events:     bus,
		}

		// Repeat 5 times (limit is 5)
		for i := 0; i < 5; i++ {
			// Change the hash to avoid text loop detection on subsequent calls
			// Note: mw(next) will calculate hash of current response and add it.
			// To bypass, we can just ensure the hash of the current response is NOT in RecentResponseHashes yet.
			_, _ = mw(next).Process(context.Background(), turn)
			assert.NotNil(t, turn.State.Response, "Should not be nil on attempt %d", i+1)

			// Manually clear RecentResponseHashes or modify them to keep bypassing text loop detection
			turn.State.RecentResponseHashes = []string{"unique" + time.Now().String() + string(rune(i))}
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
	assert.Empty(t, bus.GetEvents(), "no events should be published with cancelled context")
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

	// Use a deadline so short (1 nanosecond) that it expires before SafePublish
	// can even call bus.Publish. SafePublish wraps this in its own WithTimeout(2s),
	// but the parent context deadline takes precedence.
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	time.Sleep(time.Microsecond) // ensure the deadline has passed

	// Must not panic. The deadline triggers a context.DeadlineExceeded error
	// wrapped by SafePublish, which is NOT ErrBusNotInitialized, so the
	// error-logging branch fires.
	assert.NotPanics(t, func() {
		engine.publishTurnStatus(ctx, turn, false, false)
	})

	// Verify no event was published (deadline expired before Publish ran)
	assert.Empty(t, bus.GetEvents(), "no events should be published with expired deadline")
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
	// GetTotalCost, GetDailyCost, and GetStats on the mock.
	assert.NotPanics(t, func() {
		engine.publishTurnStatus(context.Background(), turn, false, false)
	})

	// Verify the event was published successfully (happy path)
	found := false
	for _, e := range bus.GetEvents() {
		if evt, ok := e.(events.TurnStatusEvent); ok {
			// MockCostTracker returns GetTotalCost → 0.05, GetDailyCost → 0.05,
			// GetStats → (UsageStats{}, 0.05) — UsageStats fields are all zero.
			assert.Equal(t, 0.05, evt.Status.SessionCost)
			assert.Equal(t, 0.05, evt.Status.DailyCost)
			assert.Equal(t, int64(0), evt.Status.TotalM)
			assert.Equal(t, int64(0), evt.Status.TotalH)
			assert.Equal(t, int64(0), evt.Status.TotalO)
			found = true
		}
	}
	assert.True(t, found, "TurnStatusEvent should be published")
}
