// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUIBridge_LoadShedding_NonBlocking(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mRenderer := new(mockUIRenderer)
	// Block the loop indefinitely to fill the channel
	block := make(chan struct{})
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
		<-block
	}).Return()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", logger)
	defer func() {
		close(block)
		bridge.Cleanup()
	}()

	// The channel capacity is 100.
	// 1 event is currently being processed (blocked on LogTurnStatus).
	// 100 events will fill the channel.
	for i := 0; i < 101; i++ {
		bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	}

	// The 102nd event should NOT block because of the non-blocking select with default case.
	done := make(chan struct{})
	go func() {
		bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
		close(done)
	}()

	select {
	case <-done:
		// Success: it didn't block
	case <-time.After(500 * time.Millisecond):
		t.Fatal("bridge.handleEvent blocked when channel was full; load shedding not working")
	}

	// Verify that the debug message was logged
	require.Contains(t, buf.String(), "UI Bridge queue full, shedding load/visual event")
}

func TestUIBridge_Shutdown_FastDrain(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	// 1. Setup a block to freeze the actor loop
	block := make(chan struct{})

	// 2. Setup a mock that will block the loop when a specific event is processed
	// Use Once() to ensure we only block once and let subsequent events flow.
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(_ mock.Arguments) {
		<-block
	}).Return().Once()

	// Subsequent LogTurnStatus calls should return normally
	mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()

	// 3. CRITICAL: Add .Maybe() to the method we expect NOT to be called!
	// In the uiBridge.loop shutdown branch, ResponseEvent is explicitly skipped.
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())

	// 4. Send the blocking event, then send events we want to test for fast-drain
	// This event freezes the loop
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	// This event should be SKIPPED during fast drain (shutdown)
	bridge.handleEvent(context.Background(), events.ResponseEvent{Content: &llm.Content{}})
	// This event should be PROCESSED even during fast drain
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})

	// 5. Trigger shutdown concurrently
	go bridge.Cleanup()

	// 6. Yield briefly to ensure the shutdown signal (close(b.done)) is registered by the loop's select.
	time.Sleep(20 * time.Millisecond)

	// 7. Unblock the loop, forcing it to immediately enter the fast-drain phase
	close(block)

	// Finalize Cleanup (it's safe to call multiple times or just wait for it to finish)
	bridge.Cleanup()

	// 8. Assert expected calls. LogTurnStatus should have been called twice (once blocking, once fast-drain).
	// RenderResponse should NOT have been called.
	mRenderer.AssertNotCalled(t, "RenderResponse", mock.Anything, mock.Anything, mock.Anything)
	mRenderer.AssertExpectations(t)
}

func TestUIBridge_QoSRouting(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	
	// Block the loop indefinitely to fill the channel
	block := make(chan struct{})
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
		<-block
	}).Return()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())
	defer func() {
		close(block)
		bridge.Cleanup()
	}()

	// Fill the channel (capacity 100)
	// 1 event is currently being processed (blocked on LogTurnStatus).
	// 100 events will fill the channel.
	// We send more than 101 to ensure the channel is full regardless of loop timing.
	for i := 0; i < 200; i++ {
		bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	}
	// Small sleep to ensure the loop has taken an item and blocked
	time.Sleep(100 * time.Millisecond)
	// Fill again to be absolutely sure
	for i := 0; i < 200; i++ {
		bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
	}

	t.Run("Transient event should be shed", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			bridge.handleEvent(context.Background(), events.TurnStatusEvent{})
			close(done)
		}()

		select {
		case <-done:
			// Success: it didn't block
		case <-time.After(200 * time.Millisecond):
			t.Fatal("bridge.handleEvent blocked for transient event when channel was full")
		}
	})

	t.Run("Critical event should block", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			bridge.handleEvent(context.Background(), events.ResponseEvent{})
			close(done)
		}()

		select {
		case <-done:
			t.Fatal("bridge.handleEvent did not block for critical event when channel was full")
		case <-time.After(200 * time.Millisecond):
			// Success: it blocked
		}
	})
	
	t.Run("Critical event should respect context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already cancelled
		
		done := make(chan struct{})
		go func() {
			bridge.handleEvent(ctx, events.ResponseEvent{})
			close(done)
		}()

		select {
		case <-done:
			// Success: it returned due to context cancellation
		case <-time.After(200 * time.Millisecond):
			t.Fatal("bridge.handleEvent blocked indefinitely for critical event with cancelled context")
		}
	})
}
