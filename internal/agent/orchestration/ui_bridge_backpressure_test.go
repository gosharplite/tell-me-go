// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUIBridge_LoadShedding_NonBlocking(t *testing.T) {
	t.Parallel()
	var buf syncWriter
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mRenderer := new(mockUIRenderer)
	// Block the loop indefinitely to fill the channel
	block := make(chan struct{})
	inMock := make(chan struct{}, 1)
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		select {
		case inMock <- struct{}{}:
		default:
		}
		<-block
	}).Return()

	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(logger))
	bridge.Start(context.Background())
	defer func() {
		close(block)
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// The channel capacity is 100.
	// 1 event is currently being processed (blocked on LogTurnStatus).
	_ = bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	<-inMock // Wait for the loop to block

	// 100 events will fill the channel.
	for i := 0; i < 100; i++ {
		_ = bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	}

	// The 102nd event (transient visual event) should NOT block because of the non-blocking select with default case.
	// It is natively synchronous and returns instantly if load shedding is working.
	done := make(chan struct{})
	go func() {
		_ = bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
		close(done)
	}()

	timeout := 5 * time.Second
	if deadline, ok := t.Deadline(); ok {
		timeout = time.Until(deadline) / 2
	}

	select {
	case <-done:
		// Success
	case <-time.After(timeout):
		t.Fatal("UI Bridge queue full, but handleEvent blocked unexpectedly (load-shedding failed)")
	}

	// Verify that the debug message was logged
	require.Contains(t, buf.String(), "UI Bridge queue full, shedding load/visual event")
}

func TestUIBridge_Shutdown_GracefulDrain(t *testing.T) {
	t.Parallel()
	var buf syncWriter
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mRenderer := new(mockUIRenderer)
	// 1. Setup a block to freeze the actor loop
	block := make(chan struct{})

	// 2. Setup a mock that will block the loop when a specific event is processed
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		<-block
	}).Return().Once()

	// 3. We expect all events to be processed during graceful drain
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Once()
	mRenderer.On("LogSystemMessage", mock.Anything, "processed", "warn").Return().Once()

	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(logger))
	bridge.Start(context.Background())

	// 4. Send the blocking event, then send events that MUST be drained
	_ = bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	_ = bridge.handleEvent(context.Background(), events.ResponseEvent{Content: &llm.Content{}})
	_ = bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "processed", Level: "warn"})

	// 5. Trigger shutdown concurrently
	cleanupDone := make(chan struct{})
	go func() {
		bridge.CloseInput()
		bridge.Cleanup()
		close(cleanupDone)
	}()

	// 6. Unblock the loop immediately. The pipeline is deterministic.
	close(block)

	// 7. Wait for the cleanup goroutine to finish using a channel (e.g., cleanupDone)
	select {
	case <-cleanupDone:
		// Success - drained gracefully
	case <-time.After(2 * time.Second): // Generous timeout for test failure
		t.Fatal("Cleanup did not finish even after unblocking; pipeline is deadlocked")
	}

	// 8. Assert expectations. All events should have been processed.
	mRenderer.AssertExpectations(t)
}

func TestUIBridge_QoSRouting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		event              events.Event
		expectBlocking     bool
		isContextCancelled bool
	}{
		{
			name:           "Critical TurnStatusEvent should block (enforce backpressure)",
			event:          events.TurnStatusEvent{},
			expectBlocking: true,
		},
		{
			name:           "Critical ResponseEvent should block (enforce backpressure)",
			event:          events.ResponseEvent{},
			expectBlocking: true,
		},
		{
			name:           "Critical SystemMessageEvent should block (enforce backpressure)",
			event:          events.SystemMessageEvent{Message: "critical", Level: "error"},
			expectBlocking: true,
		},
		{
			name:           "Critical ToolCallEvent should block (enforce backpressure)",
			event:          events.ToolCallEvent{Calls: []*llm.FunctionCall{{Name: "test"}}},
			expectBlocking: true,
		},
		{
			name:           "Critical ToolResultEvent should block (enforce backpressure)",
			event:          events.ToolResultEvent{Name: "test", Result: tools.ToolResult{Text: "ok"}},
			expectBlocking: true,
		},
		{
			name:           "Critical UsageMetricsEvent should block (enforce backpressure)",
			event:          events.UsageMetricsEvent{Metrics: &llm.Metrics{}, StartTime: time.Now()},
			expectBlocking: true,
		},
		{
			name:               "Critical event should respect context cancellation",
			event:              events.ResponseEvent{},
			expectBlocking:     false,
			isContextCancelled: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			timeout := 5 * time.Second
			if deadline, ok := t.Deadline(); ok {
				timeout = time.Until(deadline) / 2
			}
			mRenderer := new(mockUIRenderer)
			// Allow LogTurnStatus to be called many times as we drain the events during cleanup
			mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogToolCall", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogToolResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogUsage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

			// 1. Setup a block to freeze the actor loop
			block := make(chan struct{})
			inMock := make(chan struct{}, 1)

			// Override the first LogTurnStatus to block the loop
			mRenderer.ExpectedCalls = nil // Clear previous Maybe() for precise control
			mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				select {
				case inMock <- struct{}{}:
				default:
				}
				<-block
			}).Return().Once()
			mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogToolCall", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogToolResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogUsage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

			bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
			bridge.Start(context.Background())
			defer func() {
				select {
				case <-block:
				default:
					close(block)
				}
				bridge.CloseInput()
				bridge.Cleanup()
			}()

			// 2. Send the first event to block the loop
			_ = bridge.handleEvent(context.Background(), events.TurnStatusEvent{})

			// 3. Wait for the loop to reach the mock and block
			<-inMock

			// 4. Fill the channel (capacity 100)
			for i := 0; i < 100; i++ {
				_ = bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
			}

			// 5. Execute the test case
			ctx := context.Background()
			if tt.isContextCancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}

			// 6. Assert blocking behavior
			if tt.expectBlocking {
				done := make(chan struct{})
				go func() {
					_ = bridge.handleEvent(ctx, tt.event)
					close(done)
				}()

				// To be truly deterministic without sleeps, we check that it hasn't finished yet.
				// Given the queue is full and loop is blocked, it MUST block.
				select {
				case <-done:
					t.Fatalf("%s: bridge.handleEvent should have blocked but returned early", tt.name)
				default:
					// Proceed
				}

				// Deterministic: Unblock the queue and assert successful delivery!
				close(block)

				select {
				case <-done:
					// Success: It successfully waited and then delivered the event.
				case <-time.After(timeout):
					t.Fatalf("%s: Deadlock! Event never processed after queue unblocked", tt.name)
				}
			} else {
				// Non-blocking case: fail fast if regression causes a hang
				done := make(chan struct{})
				go func() {
					bridge.handleEvent(ctx, tt.event)
					close(done)
				}()

				select {
				case <-done:
					// Success: it load-shed or respected context cancellation and returned immediately
				case <-time.After(timeout):
					t.Fatalf("%s: Regression: Load-shedding failed, handleEvent blocked unexpectedly", tt.name)
				}
			}
		})
	}
}

func TestUIBridge_ContextCancellationMidFlight(t *testing.T) {
	t.Parallel()
	timeout := 5 * time.Second
	if deadline, ok := t.Deadline(); ok {
		timeout = time.Until(deadline) / 2
	}

	mRenderer := new(mockUIRenderer)
	// Block the loop on a critical event
	block := make(chan struct{})
	inMock := make(chan struct{}, 1)

	mRenderer.On("LogSystemMessage", mock.Anything, "BLOCK", mock.Anything).Run(func(args mock.Arguments) {
		select {
		case inMock <- struct{}{}:
		default:
		}
		<-block
	}).Return().Once()

	// Allow other messages during cleanup
	mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() {
		select {
		case <-block:
		default:
			close(block)
		}
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Send critical event to block the loop
	_ = bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "BLOCK", Level: "info"})

	select {
	case <-inMock:
		// Loop is now blocked in LogSystemMessage
	case <-time.After(timeout):
		t.Fatal("Bridge did not reach blocking mock")
	}

	// 2. Fill the channel (capacity 100)
	for i := 0; i < 100; i++ {
		// Use ResponseEvent to ensure they are not shed
		_ = bridge.handleEvent(context.Background(), events.ResponseEvent{})
	}

	// 3. Prepare an ALREADY cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// 4. Trigger call and assert it returns immediately without blocking
	done := make(chan struct{})
	go func() {
		_ = bridge.handleEvent(ctx, events.ResponseEvent{})
		close(done)
	}()

	select {
	case <-done:
		// Success: Goroutine returned immediately due to pre-cancelled context
	case <-time.After(timeout):
		t.Fatal("handleEvent did not respect cancelled context immediately")
	}
}

func TestUIBridge_HandleEvent_BridgeShutdownDuringWait(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	// Block the loop to fill the channel
	block := make(chan struct{})
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		<-block
	}).Return()

	bridge := newUIBridge(mRenderer)
	bridgeCtx := bridge.Start(context.Background())

	// 1. Block the loop
	_ = bridge.handleEvent(bridgeCtx, events.TurnStatusEvent{})

	// 2. Fill the channel (capacity 100)
	for i := 0; i < 100; i++ {
		_ = bridge.handleEvent(bridgeCtx, events.TurnStatusEvent{})
	}

	// 3. Start a goroutine that will block on sending a critical event
	done := make(chan struct{})
	go func() {
		_ = bridge.handleEvent(bridgeCtx, events.TurnStarted{})
		close(done)
	}()

	// Give it a moment to block
	time.Sleep(100 * time.Millisecond)

	// 4. Cancel the bridge context
	bridge.cancel()

	// 5. Assert handleEvent unblocks via <-ctx.Done() (where ctx is bridgeCtx)
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("handleEvent did not unblock after bridge context cancellation")
	}

	// Cleanup
	close(block)
	bridge.CloseInput()
	bridge.Cleanup()
}

func TestUIBridge_HandleEvent_AlreadyShutdown(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge := newUIBridge(mRenderer)
	bridgeCtx := bridge.Start(ctx)

	// Shutdown the bridge
	bridge.CloseInput()
	bridge.Cleanup()

	// Assert context is cancelled
	assert.Error(t, bridgeCtx.Err())

	// Attempt to send a critical event. It should hit the early return.
	// We verify it doesn't block (since channel is empty, it wouldn't anyway, but it returns early).
	assert.NotPanics(t, func() {
		_ = bridge.handleEvent(bridgeCtx, events.TurnStarted{})
	})
}
