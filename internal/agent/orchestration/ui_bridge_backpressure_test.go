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
	require.Contains(t, buf.String(), "UI Bridge queue full, shedding load")
}

func TestUIBridge_Shutdown_FastDrain(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	// We don't expect RenderResponse to be called during shutdown drain
	mRenderer.On("LogTurnStatus", mock.Anything).Return()

	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())
	
	// 1. Enqueue a visual event that should be skipped during shutdown
	bridge.handleEvent(context.Background(), events.ResponseEvent{Content: &llm.Content{}})
	// 2. Enqueue a non-visual event that should still be processed
	bridge.handleEvent(context.Background(), events.TurnStatusEvent{})

	// Trigger shutdown
	bridge.Cleanup()

	// Verification: LogTurnStatus should have been called, but RenderResponse should NOT
	mRenderer.AssertExpectations(t)
	mRenderer.AssertNotCalled(t, "RenderResponse", mock.Anything, mock.Anything, mock.Anything)
}
