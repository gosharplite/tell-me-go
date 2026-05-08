// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

// controllableUIRenderer provides synchronization hooks for testing bridge backpressure.
type controllableUIRenderer struct {
	agenttest.MockUIRenderer
	reachedCh chan struct{}
	blockCh   chan struct{}
}

func (m *controllableUIRenderer) maybeBlock(ctx context.Context) {
	select {
	case m.reachedCh <- struct{}{}:
	default:
	}
	select {
	case <-m.blockCh:
	case <-ctx.Done():
	}
}

func (m *controllableUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	m.maybeBlock(ctx)
	m.MockUIRenderer.LogTurnStatus(ctx, status)
}

func (m *controllableUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.maybeBlock(ctx)
	m.MockUIRenderer.LogSystemMessage(ctx, msg, level)
}

func (m *controllableUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
	m.maybeBlock(ctx)
	m.MockUIRenderer.RenderResponse(ctx, content, showThoughts, rawOutput)
}

func (m *controllableUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.maybeBlock(ctx)
	m.MockUIRenderer.LogUsage(ctx, metrics, logFile, startTime)
}

func (m *controllableUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.maybeBlock(ctx)
	m.MockUIRenderer.LogToolCall(ctx, calls, turn, maxTurns, showTools)
}

func (m *controllableUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
	m.maybeBlock(ctx)
	m.MockUIRenderer.LogToolResult(ctx, name, result, showTools)
}

// uiBridgeFixture encapsulates the bridge under test and its lifecycle.
type uiBridgeFixture struct {
	bridge   *Bridge
	renderer *controllableUIRenderer
	ctx      context.Context
	cancel   context.CancelFunc
	logBuf   *syncWriter
}

// startListen launches bridge.Listen in a goroutine and registers a t.Cleanup
// that fails the test if Listen returned a non-cancellation error.
//
// The returned cancel function should be deferred or called explicitly to
// shut down the listener; t.Cleanup will then read the error channel exactly
// once after Listen exits.
//
// The returned done channel closes when Listen returns. Tests that need to
// wait for Listen to exit mid-test (e.g., after a panic) can select on done.
func startListen(t *testing.T, b *Bridge) (ctx context.Context, cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()
	ctx, cancel = context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	go func() {
		defer close(errCh)
		defer close(doneCh)
		if err := b.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			// Panics recovered by the bridge's internal recovery are
			// expected when tests intentionally trigger panics. The
			// test surfaces these via the done channel instead.
			if !strings.Contains(err.Error(), "uibridge panicked:") {
				errCh <- err
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err, ok := <-errCh:
			if ok && err != nil {
				t.Errorf("Listen returned unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("Listen did not exit within 2s of cancel()")
		}
	})
	return ctx, cancel, doneCh
}

// newUIBridgeFixture initializes a bridge with a controllable renderer and starts its listen loop.
func newUIBridgeFixture(t *testing.T, opts ...bridgeOption) *uiBridgeFixture {
	t.Helper()
	renderer := &controllableUIRenderer{
		reachedCh: make(chan struct{}, 1),
		blockCh:   make(chan struct{}),
	}
	logBuf := &syncWriter{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Append logger to opts to ensure it captures output for assertions.
	opts = append(opts, WithBridgeLogger(logger))

	bridge := NewBridge(renderer, opts...)
	ctx, cancel, _ := startListen(t, bridge)

	f := &uiBridgeFixture{
		bridge:   bridge,
		renderer: renderer,
		ctx:      ctx,
		cancel:   cancel,
		logBuf:   logBuf,
	}

	t.Cleanup(func() {
		bridge.CloseInput()
		f.UnblockLoop() // Crucial: ensure bridge can drain events during Cleanup
		bridge.Cleanup()
		// cancel() is now handled by startListen's t.Cleanup, which runs
		// after this cleanup (LIFO order: fixture cleanup first, then
		// startListen cleanup calls cancel() and reads errCh).
	})

	bridge.WaitStarted()

	return f
}

// BlockLoop sends a TurnStatusEvent and waits for the renderer to be entered and blocked.
func (f *uiBridgeFixture) BlockLoop(t *testing.T) {
	t.Helper()
	f.renderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()

	if err := f.bridge.HandleEvent(context.Background(), events.TurnStatusEvent{}); err != nil {
		t.Fatalf("failed to send blocking event: %v", err)
	}

	select {
	case <-f.renderer.reachedCh:
		// Loop is now confirmed to be blocked in LogTurnStatus.
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for bridge loop to reach blocking mock")
	}
}

// UnblockLoop releases the blocked renderer method.
func (f *uiBridgeFixture) UnblockLoop() {
	select {
	case <-f.renderer.blockCh:
		// Already closed
	default:
		close(f.renderer.blockCh)
	}
}

// FillQueue pushes 100 events into the bridge to saturate its internal channel.
func (f *uiBridgeFixture) FillQueue(event events.Event) {
	for i := 0; i < 100; i++ {
		_ = f.bridge.HandleEvent(context.Background(), event)
	}
}

// AssertEventBlocks verifies that sending the event to HandleEvent blocks until the renderer is unblocked.
func (f *uiBridgeFixture) AssertEventBlocks(t *testing.T, ctx context.Context, event events.Event, timeout time.Duration, name string) {
	t.Helper()
	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		close(started)
		_ = f.bridge.HandleEvent(ctx, event)
		close(done)
	}()

	<-started

	select {
	case <-done:
		t.Fatalf("%s: expected event to block, but it returned immediately", name)
	case <-time.After(100 * time.Millisecond):
		// Successfully blocked
	}

	f.UnblockLoop()

	select {
	case <-done:
		// Success
	case <-time.After(timeout):
		t.Fatalf("%s: event did not unblock after releasing renderer", name)
	}
}

// AssertEventDoesNotBlock verifies that sending the event to HandleEvent returns immediately.
func (f *uiBridgeFixture) AssertEventDoesNotBlock(t *testing.T, ctx context.Context, event events.Event, timeout time.Duration, name string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = f.bridge.HandleEvent(ctx, event)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(timeout):
		t.Fatalf("%s: expected event NOT to block, but it timed out", name)
	}
}
