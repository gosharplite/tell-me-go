// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"errors"
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
	f := newUIBridgeFixture(t,
		withBridgeThoughts(true),
		withBridgeTools(true),
		withBridgeRawOutput(false),
		withBridgeColor(true),
		withBridgeLogFile("log.txt"),
	)
	f.BlockLoop(t)
	f.FillQueue(events.TurnStatusEvent{})

	// The 102nd event (transient visual event) should NOT block
	done := make(chan struct{})
	go func() {
		_ = f.bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
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

	require.Contains(t, f.logBuf.String(), "UI Bridge queue full, shedding load/visual event")
}

func TestUIBridge_Shutdown_GracefulDrain(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)
	f.BlockLoop(t)

	// We expect all events to be processed during graceful drain
	f.renderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Once()
	f.renderer.On("LogSystemMessage", mock.Anything, "processed", "warn").Return().Once()

	// Send events that MUST be drained
	_ = f.bridge.handleEvent(context.Background(), events.ResponseEvent{Content: &llm.Content{}})
	_ = f.bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "processed", Level: "warn"})

	// Trigger shutdown concurrently
	cleanupDone := make(chan struct{})
	go func() {
		f.bridge.CloseInput()
		f.bridge.Cleanup()
		close(cleanupDone)
	}()

	// Unblock the loop immediately.
	f.UnblockLoop()

	// Wait for the cleanup goroutine to finish
	select {
	case <-cleanupDone:
		// Success - drained gracefully
	case <-time.After(2 * time.Second): // Generous timeout for test failure
		t.Fatal("Cleanup did not finish even after unblocking; pipeline is deadlocked")
	}

	f.renderer.AssertExpectations(t)
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

			f := newUIBridgeFixture(t)
			f.renderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
			f.renderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			f.renderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			f.renderer.On("LogToolCall", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			f.renderer.On("LogToolResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
			f.renderer.On("LogUsage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

			f.BlockLoop(t)
			f.FillQueue(events.TurnStatusEvent{})

			testCtx := context.Background()
			if tt.isContextCancelled {
				var cancel context.CancelFunc
				testCtx, cancel = context.WithCancel(context.Background())
				cancel()
			}

			if tt.expectBlocking {
				f.AssertEventBlocks(t, testCtx, tt.event, timeout, tt.name)
			} else {
				f.AssertEventDoesNotBlock(t, testCtx, tt.event, timeout, tt.name)
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

	f := newUIBridgeFixture(t)
	f.renderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
	f.renderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	f.renderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	f.BlockLoop(t)
	f.FillQueue(events.ResponseEvent{})

	testCtx, testCancel := context.WithCancel(context.Background())
	testCancel()

	f.AssertEventDoesNotBlock(t, testCtx, events.ResponseEvent{}, timeout, "TestUIBridge_ContextCancellationMidFlight")
}

func TestUIBridge_HandleEvent_BridgeShutdownDuringWait(t *testing.T) {
	t.Parallel()
	f := newUIBridgeFixture(t)
	f.BlockLoop(t)
	f.FillQueue(events.TurnStatusEvent{})

	// 3. Start a goroutine that will block on sending a critical event
	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		close(started)
		_ = f.bridge.handleEvent(f.ctx, events.TurnStarted{})
		close(done)
	}()

	<-started

	// 4. Cancel the bridge context
	f.bridge.cancel()

	// 5. Assert handleEvent unblocks via <-ctx.Done()
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("handleEvent did not unblock after bridge context cancellation")
	}

	f.UnblockLoop()
}

func TestUIBridge_HandleEvent_AlreadyShutdown(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer)
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

	// Shutdown the bridge
	bridge.CloseInput()
	bridge.Cleanup()

	// Assert internal loop context is cancelled
	assert.Error(t, bridge.GetLoopContext().Err())

	// Attempt to send a critical event. It should hit the early return.
	assert.NotPanics(t, func() {
		_ = bridge.handleEvent(ctx, events.TurnStarted{})
	})
}
