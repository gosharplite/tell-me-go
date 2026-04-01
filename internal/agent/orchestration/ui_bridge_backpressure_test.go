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
	cleanupDone := make(chan struct{})
	go func() {
		bridge.Cleanup()
		close(cleanupDone)
	}()

	// 6. WAIT for Cleanup to start and cancel the context (Deterministic synchronization)
	// This replaces the flaky time.Sleep(20 * time.Millisecond).
	<-bridge.ctx.Done()

	// 7. Unblock the loop, forcing it to immediately enter the fast-drain phase
	close(block)

	// Wait for the cleanup goroutine to finish
	<-cleanupDone

	// 8. Assert expected calls. LogTurnStatus should have been called twice (once blocking, once fast-drain).
	// RenderResponse should NOT have been called.
	mRenderer.AssertNotCalled(t, "RenderResponse", mock.Anything, mock.Anything, mock.Anything)
	mRenderer.AssertExpectations(t)
}

func TestUIBridge_QoSRouting(t *testing.T) {
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

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())

	// Setup delivery tracker for the critical event
	deliveryCh := make(chan struct{})
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		close(deliveryCh)
	}).Return().Once()
	// Allow LogTurnStatus to be called many times as we drain the 100 events
	mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()

	defer func() {
		// Ensure block is closed if not already to prevent goroutine leak in test
		select {
		case <-block:
		default:
			close(block)
		}
		bridge.Cleanup()
	}()

	// 2. Send the first event to block the loop
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})

	// 3. Wait for the loop to reach the mock and block (Deterministic synchronization)
	<-inMock

	// 4. Fill the channel (capacity 100)
	// Since the loop is already blocked, subsequent sends will fill eventCh.
	// 100 events will fill the channel.
	for i := 0; i < 100; i++ {
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

	t.Run("Critical event should not block (background delivery)", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			bridge.handleEvent(context.Background(), events.ResponseEvent{})
			close(done)
		}()

		select {
		case <-done:
			// Success: it didn't block
		case <-time.After(500 * time.Millisecond):
			t.Fatal("bridge.handleEvent blocked for critical event when channel was full")
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

	// 5. Unblock the loop and verify deterministic delivery of the critical event
	close(block)

	select {
	case <-deliveryCh:
		// Success: The background goroutine successfully delivered the queued critical event!
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for critical event to be delivered after queue unblocked")
	}

	mRenderer.AssertExpectations(t)
}
