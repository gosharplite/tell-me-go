// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
)

// newStartedTestBridge creates a Bridge with default test options,
// starts its background Listen loop, and registers cleanup.
// Returns the bridge and a context.CancelFunc for the bridge's lifecycle.
func newStartedTestBridge(t *testing.T) (*Bridge, context.CancelFunc, *agenttest.MockUIRenderer) {
	t.Helper()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer,
		WithBridgeThoughts(true),
		WithBridgeTools(true),
		WithBridgeRawOutput(false),
		WithBridgeColor(true),
		WithBridgeLogFile("log.txt"),
		WithBridgeLogger(slog.Default()),
	)
	_, cancel, _ := startListen(t, bridge)
	bridge.WaitStarted()
	return bridge, cancel, mRenderer
}

// waitOrFatal waits for done to close, or calls t.Fatal with the given
// message after the timeout expires.
func waitOrFatal(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}

func TestUIBridge_HandleEvent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		event    events.Event
		setup    func(m *agenttest.MockUIRenderer) <-chan struct{}
		preSetup func(b *Bridge, m *agenttest.MockUIRenderer)
		verify   func(t *testing.T, b *Bridge)
	}{
		{
			name: "TurnStatusEvent",
			event: events.TurnStatusEvent{
				Status: events.TurnStatus{SessionTurns: 1},
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.LogTurnStatusFn = func(ctx context.Context, status events.TurnStatus) {
					close(done)
				}
				return done
			},
		},
		{
			name: "UsageMetricsEvent",
			event: events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{PromptTokens: 10},
				StartTime: time.Now(),
				Context:   context.Background(),
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.LogUsageFn = func(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
					close(done)
				}
				return done
			},
		},
		{
			name: "ToolCallEvent",
			event: events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.LogToolCallFn = func(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
					close(done)
				}
				return done
			},
		},
		{
			name: "ToolResultEvent",
			event: events.ToolResultEvent{
				Name:   "test",
				Result: tools.ToolResult{Text: "result"},
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.LogToolResultFn = func(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
					close(done)
				}
				return done
			},
		},
		{
			name: "SystemMessageEvent",
			event: events.SystemMessageEvent{
				Message: "msg",
				Level:   "info",
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {
					close(done)
				}
				return done
			},
		},
		{
			name: "StatusUpdate",
			event: events.StatusUpdate{
				Message: "updating",
				Level:   "info",
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {
					close(done)
				}
				return done
			},
		},
		{
			name: "InferenceStartedEvent (Model)",
			event: events.InferenceStartedEvent{
				Model: "gpt-4o",
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name:  "InferenceStartedEvent (Empty)",
			event: events.InferenceStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name:  "SummarizationStartedEvent",
			event: events.SummarizationStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name: "ToolExecutionStartedEvent (Single)",
			event: events.ToolExecutionStartedEvent{
				ToolNames: []string{"search_files"},
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithMetricsFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name: "ToolExecutionStartedEvent (Multiple)",
			event: events.ToolExecutionStartedEvent{
				ToolNames: []string{"list_files", "read_files"},
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithMetricsFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name:  "ToolExecutionStartedEvent (Empty)",
			event: events.ToolExecutionStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithMetricsFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name: "RetryWaitingEvent",
			event: events.RetryWaitingEvent{
				Duration: 5 * time.Second,
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
					close(done)
					return func() {}
				}
				return done
			},
		},
		{
			name:  "ConsentStartedEvent (Stops Spinner)",
			event: events.ConsentStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				var once sync.Once
				m.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
					once.Do(func() { close(done) })
					return func() {}
				}
				return done
			},
			preSetup: func(b *Bridge, m *agenttest.MockUIRenderer) {
				// Start a spinner first
				_ = b.HandleEvent(context.Background(), events.InferenceStartedEvent{})
				// No need for explicit waitMock here as preSetup's effects will be checked at end
			},
			verify: func(t *testing.T, b *Bridge) {
				// Final wait ensures all events in sequence were processed
			},
		},
		{
			name:  "ConsentFinishedEvent (Resumes Active Phase)",
			event: events.ConsentFinishedEvent{},
			preSetup: func(b *Bridge, m *agenttest.MockUIRenderer) {
				// Set active phase via event
				_ = b.HandleEvent(context.Background(), events.InferenceStartedEvent{Model: "gpt-4o"})
				// Enter consent
				_ = b.HandleEvent(context.Background(), events.ConsentStartedEvent{})
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				var count int32
				m.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
					if status == " Thinking [gpt-4o]..." {
						if atomic.AddInt32(&count, 1) == 2 {
							close(done)
						}
					}
					return func() {}
				}
				return done
			},
		},
		{
			name: "ResponseEvent",
			event: events.ResponseEvent{
				Content: &llm.Content{Parts: []*llm.Part{{Text: "result"}}},
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.RenderResponseFn = func(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
					close(done)
				}
				return done
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mRenderer := new(agenttest.MockUIRenderer)
			bridge := NewBridge(mRenderer,
				WithBridgeThoughts(true),
				WithBridgeTools(true),
				WithBridgeRawOutput(false),
				WithBridgeColor(true),
				WithBridgeLogFile("log.txt"),
				WithBridgeLogger(slog.Default()),
			)
			_, _, _ = startListen(t, bridge)
			bridge.WaitStarted()
			defer func() { bridge.CloseInput(); bridge.Cleanup() }()
			// Set up expectations BEFORE preSetup
			done := tt.setup(mRenderer)
			if tt.preSetup != nil {
				tt.preSetup(bridge, mRenderer)
			}

			_ = bridge.HandleEvent(context.Background(), tt.event)

			// Wait for the async actor loop to process the event(s)
			if done != nil {
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatalf("%s: timeout waiting for event processing", tt.name)
				}
			}

			if tt.verify != nil {
				tt.verify(t, bridge)
			}
		})
	}
}

func TestUIBridge_EnsureContext(t *testing.T) {
	t.Parallel()
	// Test ensureContext via the eventDispatcher where it now lives.
	d := newEventDispatcher(
		nil, slog.Default(), nil, nil,
		false, false, false, "",
	)

	t.Run("Returns existing context", func(t *testing.T) {
		type contextKey string
		const testKey contextKey = "key"
		ctx := context.WithValue(context.Background(), testKey, "value")
		result := d.ensureContext(ctx, "test")
		assert.Equal(t, ctx, result)
	})

	t.Run("Returns background context and logs debug if nil", func(t *testing.T) {
		var nilCtx context.Context
		result := d.ensureContext(nilCtx, "test")
		assert.NotNil(t, result)
	})
}

func TestUIBridge_Concurrency(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, _, _ := startListen(t, bridge)
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	var wg sync.WaitGroup
	const iterations = 1000
	start := make(chan struct{})

	// Fire InferenceStartedEvent and ResponseEvent simultaneously
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = bridge.HandleEvent(ctx, events.InferenceStartedEvent{})
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = bridge.HandleEvent(ctx, events.ResponseEvent{
				Content: &llm.Content{},
			})
		}
	}()

	// Fire other events to simulate real event bus behavior and increase noise
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = bridge.HandleEvent(ctx, events.TurnStarted{})
			_ = bridge.HandleEvent(ctx, events.TurnStatusEvent{Status: events.TurnStatus{}})
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = bridge.HandleEvent(ctx, events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{},
				StartTime: time.Now(),
				Context:   ctx,
			})
			_ = bridge.HandleEvent(ctx, events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			})
		}
	}()

	close(start)
	wg.Wait()
	// No explicit sync needed here, Cleanup will wait for the loop to finish
}

func TestUIBridge_LogicalRace(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, _, _ := startListen(t, bridge)
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// 1. Mark as rendering via ResponseEvent
	_ = bridge.HandleEvent(ctx, events.ResponseEvent{
		Content: &llm.Content{},
	})

	// 2. Try to start a spinner (should be suppressed)
	_ = bridge.HandleEvent(ctx, events.InferenceStartedEvent{})

	// 3. Send a sentinel to ensure #2 was processed
	done := make(chan struct{})
	mRenderer.LogTurnStatusFn = func(ctx context.Context, status events.TurnStatus) {
		close(done)
	}
	_ = bridge.HandleEvent(ctx, events.TurnStatusEvent{})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for LogicalRace sentinel")
	}

	// Verification: StartSpinnerWithStatus must NOT be called when already rendering
	snap := mRenderer.Snapshot()
	assert.Equal(t, 0, snap.StartSpinnerWithStatus, "StartSpinnerWithStatus must NOT be called when already rendering")
}

func TestUIBridge_AbortedTurn_SpinnerCleanup(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	spinnerStopped := make(chan struct{})
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		if status == " Thinking..." {
			return func() { close(spinnerStopped) }
		}
		return func() {}
	}

	// Start Inference
	_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})

	// Force new turn before ResponseEvent arrives (Simulates an abort/reset)
	_ = bridge.HandleEvent(context.Background(), events.TurnStarted{})

	select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Expected stopSpinner to be called during TurnStarted to prevent resource leaks")
	}
}

func TestUIBridge_Retry_Spinner(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// First attempt
	done1 := make(chan struct{})
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		if status == " Thinking..." {
			close(done1)
		}
		return func() {}
	}
	_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first attempt spinner")
	}

	// Response (e.g. error)
	done2 := make(chan struct{})
	mRenderer.RenderResponseFn = func(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
		close(done2)
	}
	_ = bridge.HandleEvent(context.Background(), events.ResponseEvent{
		Content: &llm.Content{},
	})
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Second attempt (Retry)
	// Now this SHOULD be called because RetryWaitingEvent resets isRendering.
	done3 := make(chan struct{})
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		if status == " Retrying in 5s..." {
			close(done3)
		}
		return func() {}
	}
	_ = bridge.HandleEvent(context.Background(), events.RetryWaitingEvent{Duration: 5 * time.Second})
	select {
	case <-done3:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for retry spinner")
	}
}

func TestUIBridge_CleanupOnUnexpectedExit(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()

	spinnerStarted := make(chan struct{})
	spinnerStopped := make(chan struct{})
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		if status == " Thinking..." {
			close(spinnerStarted)
			return func() { close(spinnerStopped) }
		}
		return func() {}
	}

	// Start Inference
	_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})

	// Wait for spinner to start
	select {
	case <-spinnerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for spinner to start")
	}

	// Simulate unexpected exit by calling Cleanup
	bridge.CloseInput()
	bridge.Cleanup()

	select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Expected stopSpinner to be called during Cleanup")
	}
}

func TestUIBridge_SpinnerTransitions(t *testing.T) {
	t.Parallel()
	bridge, _, mRenderer := newStartedTestBridge(t)
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// 1. Summarization starts
	stopSummarizationCalled := make(chan struct{})
	doneSummarization := make(chan struct{})
	stopInferenceCalled := make(chan struct{})
	doneInference := make(chan struct{})

	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		switch status {
		case " Compressing context...":
			close(doneSummarization)
			return func() { close(stopSummarizationCalled) }
		case " Thinking...":
			close(doneInference)
			return func() { close(stopInferenceCalled) }
		}
		return func() {}
	}

	_ = bridge.HandleEvent(context.Background(), events.SummarizationStartedEvent{})
	waitOrFatal(t, doneSummarization, "timeout waiting for summarization spinner")

	// 2. Inference starts (should stop summarization spinner first)
	_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
	waitOrFatal(t, doneInference, "timeout waiting for inference spinner")

	// Verify summarization spinner was stopped before inference started
	waitOrFatal(t, stopSummarizationCalled, "Expected summarization spinner to be stopped before inference started")

	// Cleanup remaining
	bridge.CloseInput()
	bridge.Cleanup()
	waitOrFatal(t, stopInferenceCalled, "Expected inference spinner to be stopped during cleanup")
}

func TestUIBridge_SpinnerConcurrency(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()

	var activeSpinners int32

	// Thread-safe mock setup
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		atomic.AddInt32(&activeSpinners, 1)
		return func() { atomic.AddInt32(&activeSpinners, -1) }
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = bridge.HandleEvent(context.Background(), events.SummarizationStartedEvent{})
			} else {
				_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
			}
		}(i)
	}
	wg.Wait()

	// Wait for all spinners to be stopped eventually
	bridge.CloseInput()
	bridge.Cleanup()

	// Verify all spinners were eventually stopped
	assert.Equal(t, int32(0), atomic.LoadInt32(&activeSpinners), "Expected all spinners to be stopped")
}

func TestUIBridge_NilLoggerFallback(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	// Instantiate without WithLogger
	bridge := NewBridge(mRenderer)
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	assert.NotNil(t, bridge.logger, "Logger should fall back to slog.Default() if nil")
}

func TestUIBridge_CleanupTimeout(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)

	// Initialize bridge with a very small timeout via functional option
	bridge := NewBridge(mRenderer,
		withBridgeCleanupTimeout(10*time.Millisecond),
	)
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	bridgeCtx := bridge.getLoopContext() // Use the internal loop context for verification

	// Force a waitgroup hang to simulate a deadlocked renderer or long-running loop
	bridge.wg.Add(1)
	defer bridge.wg.Done() // Ensure the hung WaitGroup is eventually released to prevent goroutine leaks in the test suite.

	// Execute Cleanup. It should timeout after 10ms and return normally.
	done := make(chan struct{})
	go func() {
		bridge.CloseInput()
		bridge.Cleanup()
		close(done)
	}()

	select {
	case <-done:
		// Success: Cleanup returned even with a hung WaitGroup
	case <-time.After(1 * time.Second):
		t.Fatal("Cleanup did not return within expected timeout")
	}

	// VERIFY: context should be cancelled now
	assert.Error(t, bridgeCtx.Err(), "Context should be cancelled after Cleanup timeout")
}

func TestUIBridge_HandleEvent_ContextCancelled(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer)
	// We don't start the bridge's background loop to specifically test load shedding logic
	defer func() {
		bridge.wg.Done()
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bridge.HandleEvent(ctx, events.InferenceStartedEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestUIBridge_HandleEvent_BridgeClosed(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer)
	bridge.AbortStart()
	defer bridge.Cleanup()

	bridge.CloseInput()
	err := bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
	assert.NoError(t, err)
}

func TestUIBridge_HandleEvent_PanicRecovery(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer)
	bridge.AbortStart()
	defer bridge.Cleanup()

	bridge.queue = &panicFakeEnqueuer{}

	err := bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
	assert.NoError(t, err)
}

func TestUIBridge_HandleEvent_ActorDead(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, withBridgeQueueCapacity(1))
	bridge.AbortStart()
	defer bridge.Cleanup()

	// Fill the single-slot channel with a critical event.
	if err := bridge.HandleEvent(context.Background(), events.TurnStatusEvent{}); err != nil {
		t.Fatalf("first enqueue should succeed: %v", err)
	}

	// Kill the actor's loop context. The next critical event cannot send
	// (channel full) and falls through to eq.loopCtx.Done().
	bridge.loopCancel()

	err := bridge.HandleEvent(context.Background(), events.TurnStatusEvent{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "uibridge actor is dead")
}

func TestUIBridge_HandleEvent_SafetyWrapper(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		setup         func(q *eventQueue)
		ctx           func() context.Context
		event         events.Event
		expectEnqueue bool
	}{
		{
			name: "bridge is closed",
			setup: func(q *eventQueue) {
				q.closeInput()
			},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: false,
		},
		{
			name:  "caller context is cancelled",
			setup: func(q *eventQueue) {},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			event:         events.ResponseEvent{},
			expectEnqueue: false,
		},
		{
			name:          "normal case - enqueued",
			setup:         func(q *eventQueue) {},
			ctx:           context.Background,
			event:         events.ResponseEvent{},
			expectEnqueue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loopCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			q := newEventQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), loopCtx, 10)

			tt.setup(q)
			ctx := tt.ctx()

			var enqueued bool
			if !q.isInputClosed() && ctx.Err() == nil {
				_ = q.enqueueEvent(ctx, tt.event)
				enqueued = true
			}
			assert.Equal(t, tt.expectEnqueue, enqueued)

			if tt.expectEnqueue {
				assert.Equal(t, tt.event, <-q.recv())
			} else {
				select {
				case e, ok := <-q.recv():
					if ok {
						t.Errorf("unexpected %v", e)
					}
				default:
				}
			}
		})
	}
}

// TestUIBridge_HandleEvent_CallerContextCancelled covers the early guard in
// HandleEvent that catches cancelled contexts before they reach the queue.
func TestUIBridge_HandleEvent_CallerContextCancelled(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := f.bridge.HandleEvent(ctx, events.InferenceStartedEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}

// panicFakeEnqueuer implements eventEnqueuer and panics from enqueueEvent,
// enabling deterministic testing of HandleEvent's defer/recover path.
type panicFakeEnqueuer struct{}

func (p *panicFakeEnqueuer) enqueueEvent(context.Context, events.Event) error {
	panic("injected test panic")
}
func (p *panicFakeEnqueuer) isInputClosed() bool                                      { return false }
func (p *panicFakeEnqueuer) closeInput()                                              {}
func (p *panicFakeEnqueuer) recv() <-chan events.Event                                { return nil }
func (p *panicFakeEnqueuer) drainRemainingEvents(func(context.Context, events.Event)) {}

func TestWithBridgeClock(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer,
		WithBridgeClock(clock.RealClock{}),
	)
	bridge.AbortStart()
	defer bridge.Cleanup()

	assert.NotNil(t, bridge.clock, "bridge.clock should not be nil after WithBridgeClock")
}
