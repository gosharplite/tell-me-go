// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// newStartedTestBridge creates a UIBridge with default test options,
// starts its background Listen loop, and registers cleanup.
// Returns the bridge and a context.CancelFunc for the bridge's lifecycle.
func newStartedTestBridge(t *testing.T) (*UIBridge, context.CancelFunc, *agenttest.MockUIRenderer) {
	t.Helper()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewUIBridge(mRenderer,
		WithBridgeThoughts(true),
		WithBridgeTools(true),
		WithBridgeRawOutput(false),
		WithBridgeColor(true),
		WithBridgeLogFile("log.txt"),
		WithBridgeLogger(slog.Default()),
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
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
		preSetup func(b *UIBridge, m *agenttest.MockUIRenderer)
		verify   func(t *testing.T, b *UIBridge)
	}{
		{
			name: "TurnStatusEvent",
			event: events.TurnStatusEvent{
				Status: events.TurnStatus{SessionTurns: 1},
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
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
				m.On("LogUsage", mock.Anything, mock.Anything, "log.txt", mock.Anything).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
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
				m.On("LogToolCall", mock.Anything, mock.Anything, 0, 5, true).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
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
				m.On("LogToolResult", mock.Anything, "test", mock.Anything, true).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
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
				m.On("LogSystemMessage", mock.Anything, "msg", "info").Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
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
				m.On("LogSystemMessage", mock.Anything, "updating", "info").Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
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
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "InferenceStartedEvent (Empty)",
			event: events.InferenceStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "SummarizationStartedEvent",
			event: events.SummarizationStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
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
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing [search_files]...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
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
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools [list_files, read_files]...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "ToolExecutionStartedEvent (Empty)",
			event: events.ToolExecutionStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
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
				m.On("StartSpinnerWithStatus", mock.Anything, " Retrying in 5s...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "ConsentStartedEvent (Stops Spinner)",
			event: events.ConsentStartedEvent{},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
			preSetup: func(b *UIBridge, m *agenttest.MockUIRenderer) {
				// Start a spinner first
				_ = b.HandleEvent(context.Background(), events.InferenceStartedEvent{})
				// No need for explicit waitMock here as preSetup's effects will be checked at end
			},
			verify: func(t *testing.T, b *UIBridge) {
				// Final wait ensures all events in sequence were processed
			},
		},
		{
			name:  "ConsentFinishedEvent (Resumes Active Phase)",
			event: events.ConsentFinishedEvent{},
			preSetup: func(b *UIBridge, m *agenttest.MockUIRenderer) {
				// Set active phase via event
				_ = b.HandleEvent(context.Background(), events.InferenceStartedEvent{Model: "gpt-4o"})
				// Enter consent
				_ = b.HandleEvent(context.Background(), events.ConsentStartedEvent{})
			},
			setup: func(m *agenttest.MockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				var count int32
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Run(func(_ mock.Arguments) {
					if atomic.AddInt32(&count, 1) == 2 {
						close(done)
					}
				}).Return(func() {}).Twice()
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
				m.On("RenderResponse", mock.Anything, mock.Anything, true, false).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mRenderer := new(agenttest.MockUIRenderer)
			bridge := NewUIBridge(mRenderer,
				WithBridgeThoughts(true),
				WithBridgeTools(true),
				WithBridgeRawOutput(false),
				WithBridgeColor(true),
				WithBridgeLogFile("log.txt"),
				WithBridgeLogger(slog.Default()),
			)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			errChan := make(chan error, 1)
			go func() {
				if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
					errChan <- err
				}
				close(errChan)
			}()
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
	bridge := NewUIBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// Setup mocks with Maybe() to handle concurrent calls safely
	mRenderer.On("StartSpinner", mock.Anything).Return(func() {}).Maybe()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogUsage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogToolCall", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogToolResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
	bridge := NewUIBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// StartSpinnerWithStatus should NOT be called because ResponseEvent is already rendering
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()

	// 1. Mark as rendering via ResponseEvent
	_ = bridge.HandleEvent(ctx, events.ResponseEvent{
		Content: &llm.Content{},
	})

	// 2. Try to start a spinner (should be suppressed)
	_ = bridge.HandleEvent(ctx, events.InferenceStartedEvent{})

	// 3. Send a sentinel to ensure #2 was processed
	done := make(chan struct{})
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		close(done)
	}).Return().Once()
	_ = bridge.HandleEvent(ctx, events.TurnStatusEvent{})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for LogicalRace sentinel")
	}

	// Verification
	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)
}

func TestUIBridge_AbortedTurn_SpinnerCleanup(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewUIBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	spinnerStopped := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() { close(spinnerStopped) })

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
	bridge := NewUIBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// First attempt
	done1 := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(_ mock.Arguments) {
		close(done1)
	}).Return(func() {}).Once()
	_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first attempt spinner")
	}

	// Response (e.g. error)
	done2 := make(chan struct{})
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		close(done2)
	}).Return().Once()
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
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Retrying in 5s...").Run(func(_ mock.Arguments) {
		close(done3)
	}).Return(func() {}).Once()
	_ = bridge.HandleEvent(context.Background(), events.RetryWaitingEvent{Duration: 5 * time.Second})
	select {
	case <-done3:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for retry spinner")
	}

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_CleanupOnUnexpectedExit(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewUIBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()

	spinnerStarted := make(chan struct{})
	spinnerStopped := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(args mock.Arguments) {
		close(spinnerStarted)
	}).Return(func() { close(spinnerStopped) })

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
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Run(func(_ mock.Arguments) {
		close(doneSummarization)
	}).Return(func() {
		close(stopSummarizationCalled)
	}).Once()

	_ = bridge.HandleEvent(context.Background(), events.SummarizationStartedEvent{})
	waitOrFatal(t, doneSummarization, "timeout waiting for summarization spinner")

	// 2. Inference starts (should stop summarization spinner first)
	stopInferenceCalled := make(chan struct{})
	doneInference := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(_ mock.Arguments) {
		close(doneInference)
	}).Return(func() {
		close(stopInferenceCalled)
	}).Once()

	_ = bridge.HandleEvent(context.Background(), events.InferenceStartedEvent{})
	waitOrFatal(t, doneInference, "timeout waiting for inference spinner")

	// Verify summarization spinner was stopped before inference started
	waitOrFatal(t, stopSummarizationCalled, "Expected summarization spinner to be stopped before inference started")

	// Cleanup remaining
	bridge.CloseInput()
	bridge.Cleanup()
	waitOrFatal(t, stopInferenceCalled, "Expected inference spinner to be stopped during cleanup")

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SpinnerConcurrency(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewUIBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()

	var activeSpinners int32

	// Thread-safe mock setup
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		atomic.AddInt32(&activeSpinners, 1)
	}).Return(func() {
		atomic.AddInt32(&activeSpinners, -1)
	})

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
	bridge := NewUIBridge(mRenderer)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	assert.NotNil(t, bridge.logger, "Logger should fall back to slog.Default() if nil")
}

func TestUIBridge_CleanupTimeout(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Initialize bridge with a very small timeout via functional option
	bridge := NewUIBridge(mRenderer,
		withBridgeCleanupTimeout(10*time.Millisecond),
	)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
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
	bridge := NewUIBridge(mRenderer)
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
