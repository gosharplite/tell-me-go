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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestUIBridge_LoadShedding_NonBlocking(t *testing.T) {
	var buf syncWriter
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mRenderer := new(mockUIRenderer)
	// Block the loop indefinitely to fill the channel
	block := make(chan struct{})
	inMock := make(chan struct{}, 1)
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
		select {
		case inMock <- struct{}{}:
		default:
		}
		<-block
	}).Return()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", logger)
	defer func() {
		close(block)
		bridge.Cleanup()
	}()

	// The channel capacity is 100.
	// 1 event is currently being processed (blocked on LogTurnStatus).
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	<-inMock // Wait for the loop to block

	// 100 events will fill the channel.
	for i := 0; i < 100; i++ {
		bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	}

	// The 102nd event should NOT block because of the non-blocking select with default case.
	// It is natively synchronous and returns instantly if load shedding is working.
	done := make(chan struct{})
	go func() {
		bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("UI Bridge queue full, but handleEvent blocked unexpectedly (load-shedding failed)")
	}

	// Verify that the debug message was logged
	require.Contains(t, buf.String(), "UI Bridge queue full, shedding load/visual event")
}

func TestUIBridge_Shutdown_FastDrain(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	// 1. Setup a block to freeze the actor loop
	block := make(chan struct{})

	// 2. Setup a mock that will block the loop when a specific event is processed
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(_ mock.Arguments) {
		<-block
	}).Return().Once()

	// Subsequent LogTurnStatus calls should return normally
	mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()

	// 3. Expect RenderResponse and LogSystemMessage to be CALLED during drain phase
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Once()
	mRenderer.On("LogSystemMessage", "critical shutdown warning", mock.Anything).Return().Once()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())

	// 4. Send the blocking event, then send events we want to test for fast-drain
	// This event freezes the loop
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	// This event should NOT be skipped anymore during fast drain
	bridge.handleEvent(context.Background(), events.ResponseEvent{Content: &llm.Content{}})
	// This event should NOT be skipped during fast drain (critical system message)
	bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "critical shutdown warning", Level: "warn"})
	// This event should be skipped during fast drain (spinner)
	bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
	// This event should be PROCESSED even during fast drain
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})

	// 5. Trigger shutdown concurrently
	cleanupDone := make(chan struct{})
	go func() {
		bridge.Cleanup()
		close(cleanupDone)
	}()

	// 6. WAIT for Cleanup to start and cancel the context
	<-bridge.ctx.Done()

	// 7. Unblock the loop, forcing it to immediately enter the fast-drain phase
	close(block)

	// Wait for the cleanup goroutine to finish
	<-cleanupDone

	// 8. Assert expected calls. RenderResponse should have been called!
	mRenderer.AssertExpectations(t)
}

func TestUIBridge_QoSRouting(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name              string
		event             events.Event
		expectBlocking    bool
		isContextCancelled bool
	}{
		{
			name:           "Transient event should be shed (non-blocking)",
			event:          events.TurnStatusEvent{},
			expectBlocking: false,
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
			name:              "Critical event should respect context cancellation",
			event:             events.ResponseEvent{},
			expectBlocking:    false,
			isContextCancelled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mRenderer := new(mockUIRenderer)
			// Allow LogTurnStatus to be called many times as we drain the events during cleanup
			mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()
			mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything).Return().Maybe()

			// 1. Setup a block to freeze the actor loop
			block := make(chan struct{})
			inMock := make(chan struct{}, 1)

			// Override the first LogTurnStatus to block the loop
			mRenderer.ExpectedCalls = nil // Clear previous Maybe() for precise control
			mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
				select {
				case inMock <- struct{}{}:
				default:
				}
				<-block
			}).Return().Once()
			mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()
			mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything).Return().Maybe()

			bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())
			defer func() {
				select {
				case <-block:
				default:
					close(block)
				}
				bridge.Cleanup()
			}()

			// 2. Send the first event to block the loop
			bridge.handleEvent(context.Background(), events.TurnStatusEvent{})

			// 3. Wait for the loop to reach the mock and block
			<-inMock

			// 4. Fill the channel (capacity 100)
			for i := 0; i < 100; i++ {
				bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
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
					bridge.handleEvent(ctx, tt.event)
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
				case <-time.After(2 * time.Second):
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
				case <-time.After(1 * time.Second):
					t.Fatalf("%s: Regression: Load-shedding failed, handleEvent blocked unexpectedly", tt.name)
				}
			}
		})
	}
}

func TestUIBridge_ContextCancellationMidFlight(t *testing.T) {
	defer goleak.VerifyNone(t)

	mRenderer := new(mockUIRenderer)
	// Block the loop on a critical event
	block := make(chan struct{})
	inMock := make(chan struct{}, 1)

	mRenderer.On("LogSystemMessage", "BLOCK", mock.Anything).Run(func(args mock.Arguments) {
		select {
		case inMock <- struct{}{}:
		default:
		}
		<-block
	}).Return().Once()

	// Allow other messages during cleanup
	mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())
	defer func() {
		select {
		case <-block:
		default:
			close(block)
		}
		bridge.Cleanup()
	}()

	// 1. Send critical event to block the loop
	bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "BLOCK", Level: "info"})

	select {
	case <-inMock:
		// Loop is now blocked in LogSystemMessage
	case <-time.After(2 * time.Second):
		t.Fatal("Bridge did not reach blocking mock")
	}

	// 2. Fill the channel (capacity 100)
	for i := 0; i < 100; i++ {
		// Use ResponseEvent to ensure they are not shed
		bridge.handleEvent(context.Background(), events.ResponseEvent{})
	}

	// 3. Prepare an ALREADY cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// 4. Trigger call and assert it returns immediately without blocking
	done := make(chan struct{})
	go func() {
		bridge.handleEvent(ctx, events.ResponseEvent{})
		close(done)
	}()

	select {
	case <-done:
		// Success: Goroutine returned immediately due to pre-cancelled context
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleEvent did not respect cancelled context immediately")
	}
}
