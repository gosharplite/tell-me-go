// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type panicMockRenderer struct {
	mu          sync.Mutex
	shouldPanic bool
	lastMsg     string
	lastLevel   string
}

func (m *panicMockRenderer) StartSpinner(ctx context.Context) func() { return func() {} }
func (m *panicMockRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldPanic {
		panic("simulated ui panic")
	}
	return func() {}
}
func (m *panicMockRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	return func() {}
}
func (m *panicMockRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
}
func (m *panicMockRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {}
func (m *panicMockRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
}
func (m *panicMockRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
}
func (m *panicMockRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
}
func (m *panicMockRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMsg = msg
	m.lastLevel = level
}
func (m *panicMockRenderer) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {}
func (m *panicMockRenderer) IsTerminalContext() bool                                            { return false }
func (m *panicMockRenderer) SetUseColor(use bool)                                               {}
func (m *panicMockRenderer) SetForceSpinner(force bool)                                         {}

func TestUIBridge_PanicResilience(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &panicMockRenderer{}

	// Synchronous bus for deterministic testing
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	events.CleanupBus(t, bus)

	// Silence noise in test output
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := session.NewUIBridge(mock, session.WithBridgeThoughts(true), session.WithBridgeTools(true), session.WithBridgeRawOutput(false), session.WithBridgeColor(true), session.WithBridgeLogFile("test.log"), session.WithBridgeLogger(logger))
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		defer close(done)
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
	}()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		_ = bridge.HandleEvent(ctx, e)
	})

	// Phase 1: The Panic
	mock.mu.Lock()
	mock.shouldPanic = true
	mock.mu.Unlock()
	err := bus.Publish(ctx, events.InferenceStartedEvent{Model: "test-model"})

	// Now that it's asynchronous, bus.Publish doesn't return an error from the actor's panic.
	assert.NoError(t, err)

	// In the new implementation, a panic triggers a shutdown.
	// So we expect the bridge to be done.
	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for bridge to shutdown after panic")
	}
}

func TestUIBridge_PanicRecoveryLogging(t *testing.T) {
	t.Parallel()

	// Create a custom slog handler to capture the panic log.
	// We use LevelDebug to ensure the stack trace log is captured.
	logBuffer := testfixtures.NewSafeBuffer()
	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mockRenderer := new(agenttest.MockUIRenderer)
	// This mock will panic when StartSpinnerWithStatus is called
	mockRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		panic("intentional test panic")
	}).Return(func() {})

	bridge := session.NewUIBridge(mockRenderer, session.WithBridgeThoughts(true), session.WithBridgeTools(true), session.WithBridgeRawOutput(false), session.WithBridgeColor(true), session.WithBridgeLogFile("test.log"), session.WithBridgeLogger(logger))
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		defer close(done)
		if err := bridge.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
	}()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// Trigger the panic
	_ = bridge.HandleEvent(ctx, events.InferenceStartedEvent{})

	// Wait for shutdown and check logs
	select {
	case <-done:
		// Expected shutdown
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for bridge to shutdown after panic")
	}

	output := logBuffer.String()
	assert.Contains(t, output, "UIBridge actor recovered from panic")
	assert.Contains(t, output, "intentional test panic")
	assert.Contains(t, output, "stack")
}

func TestUIBridge_PanicInStopSpinner(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mRenderer := new(agenttest.MockUIRenderer)
	// Mock the renderer to return a closure that panics when called.
	// We use .Maybe() because SyncBridge might trigger resumeActiveSpinner which calls this again.
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {
		panic("renderer panic in stop closure")
	}).Maybe()

	// Silence noise in test output
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := session.NewUIBridge(mRenderer, session.WithBridgeThoughts(true), session.WithBridgeTools(true), session.WithBridgeRawOutput(false), session.WithBridgeColor(true), session.WithBridgeLogFile("test.log"), session.WithBridgeLogger(logger))
	done := make(chan struct{})
	testCtx, testCancel := context.WithCancel(context.Background())
	t.Cleanup(testCancel)
	errChan := make(chan error, 1)
	go func() {
		defer close(done)
		if err := bridge.Listen(testCtx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
	}()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Start a spinner to set b.stopSpinner.
	_ = bridge.HandleEvent(ctx, events.InferenceStartedEvent{Model: "test-model"})

	// 2. Wait for the event to be processed and b.stopSpinner to be set.
	// Use SyncBridge to ensure the first event is fully processed.
	session.SyncBridge(t, bridge, mRenderer)

	// 3. Trigger a primary panic.
	// This will trigger the recovery block, which calls b.stopActiveSpinner().
	// That call will trigger the double-panic.
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		panic("primary panic")
	}).Once()

	_ = bridge.HandleEvent(ctx, events.TurnStatusEvent{Status: events.TurnStatus{Mode: "test"}})

	// 4. Assert that the UIBridge survives and cancels successfully.
	select {
	case <-done:
		// Success: Bridge cancelled gracefully despite double-panic
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for bridge to shutdown after double-panic")
	}

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_PoisonPill(t *testing.T) {
	t.Parallel()

	logBuffer := testfixtures.NewSafeBuffer()
	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mRenderer := new(agenttest.MockUIRenderer)
	// First event panics
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		panic("first panic")
	}).Once()

	bridge := session.NewUIBridge(mRenderer, session.WithBridgeThoughts(true), session.WithBridgeTools(true), session.WithBridgeRawOutput(false), session.WithBridgeColor(true), session.WithBridgeLogFile("test.log"), session.WithBridgeLogger(logger))
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

	// Send two events
	_ = bridge.HandleEvent(ctx, events.TurnStatusEvent{})
	_ = bridge.HandleEvent(ctx, events.ResponseEvent{})

	// Wait for shutdown and cleanup
	bridge.CloseInput()
	bridge.Cleanup()

	output := logBuffer.String()
	// Should contain the first panic
	assert.Contains(t, output, "UIBridge actor recovered from panic")
	assert.Contains(t, output, "first panic")

	// Verify that RenderResponse was NOT called (it was the second event in the queue)
	mRenderer.AssertNotCalled(t, "RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestUIBridge_SendToClosedChannel(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	bridge := session.NewUIBridge(mRenderer, session.WithBridgeThoughts(true), session.WithBridgeTools(true), session.WithBridgeRawOutput(false), session.WithBridgeColor(true), session.WithBridgeLogFile("test.log"), session.WithBridgeLogger(logger))
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

	// Close the input to simulate a shutdown sequence.
	bridge.CloseInput()

	// Attempt to send an event after the channel is closed.
	// This should trigger the panic recovery and return without crashing.
	assert.NotPanics(t, func() {
		_ = bridge.HandleEvent(ctx, events.ResponseEvent{})
	})

	// Ensure that critical events also don't panic.
	assert.NotPanics(t, func() {
		_ = bridge.HandleEvent(ctx, events.TurnStarted{})
	})

	// Ensure that transient events also don't panic.
	assert.NotPanics(t, func() {
		_ = bridge.HandleEvent(ctx, events.InferenceStartedEvent{})
	})

	// Clean up.
	bridge.Cleanup()
}
