// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

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
func (m *panicMockRenderer) RenderResponse(content *llm.Content, showThoughts, rawOutput bool) {}
func (m *panicMockRenderer) LogTurnStatus(status events.TurnStatus)                            {}
func (m *panicMockRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
}
func (m *panicMockRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
}
func (m *panicMockRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {}
func (m *panicMockRenderer) LogSystemMessage(msg string, level string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMsg = msg
	m.lastLevel = level
}
func (m *panicMockRenderer) SetUseColor(use bool)       {}
func (m *panicMockRenderer) SetForceSpinner(force bool) {}

func TestUIBridge_PanicResilience(t *testing.T) {
	ctx := context.Background()
	mock := &panicMockRenderer{}

	// Synchronous bus for deterministic testing
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	// Silence noise in test output
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := newUIBridge(ctx, mock, true, true, false, true, "test.log", logger)
	defer bridge.Cleanup()
	bus.Subscribe(bridge.handleEvent)

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
	case <-bridge.ctx.Done():
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for bridge to shutdown after panic")
	}
}

func TestUIBridge_PanicRecoveryLogging(t *testing.T) {
	ctx := context.Background()

	// Create a custom slog handler to capture the panic log.
	// We use LevelDebug to ensure the stack trace log is captured.
	logBuffer := inframock.NewSafeBuffer()
	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mockRenderer := new(mockUIRenderer)
	// This mock will panic when StartSpinnerWithStatus is called
	mockRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		panic("intentional test panic")
	}).Return(func() {})

	bridge := newUIBridge(ctx, mockRenderer, true, true, false, true, "test.log", logger)
	defer bridge.Cleanup()

	// Trigger the panic
	bridge.handleEvent(ctx, events.InferenceStartedEvent{})

	// Wait for shutdown and check logs
	select {
	case <-bridge.ctx.Done():
		// Expected shutdown
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for bridge to shutdown after panic")
	}

	output := logBuffer.String()
	assert.Contains(t, output, "uiBridge actor recovered from panic")
	assert.Contains(t, output, "intentional test panic")
	assert.Contains(t, output, "stack")
}

func TestUIBridge_PanicInStopSpinner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mRenderer := new(mockUIRenderer)
	// Mock the renderer to return a closure that panics when called.
	// We use .Maybe() because syncBridge might trigger resumeActiveSpinner which calls this again.
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {
		panic("renderer panic in stop closure")
	}).Maybe()

	// Silence noise in test output
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := newUIBridge(ctx, mRenderer, true, true, false, true, "test.log", logger)
	defer bridge.Cleanup()

	// 1. Start a spinner to set b.stopSpinner.
	bridge.handleEvent(ctx, events.InferenceStartedEvent{Model: "test-model"})

	// 2. Wait for the event to be processed and b.stopSpinner to be set.
	// Use syncBridge to ensure the first event is fully processed.
	syncBridge(t, bridge, mRenderer)

	// 3. Trigger a primary panic.
	// This will trigger the recovery block, which calls b.stopActiveSpinner().
	// That call will trigger the double-panic.
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
		panic("primary panic")
	}).Once()

	bridge.handleEvent(ctx, events.TurnStatusEvent{Status: events.TurnStatus{Mode: "test"}})

	// 4. Assert that the uiBridge survives and cancels successfully.
	select {
	case <-bridge.ctx.Done():
		// Success: Bridge cancelled gracefully despite double-panic
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for bridge to shutdown after double-panic")
	}

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_PoisonPill(t *testing.T) {
	ctx := context.Background()

	logBuffer := inframock.NewSafeBuffer()
	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mRenderer := new(mockUIRenderer)
	// First event panics
	mRenderer.On("LogTurnStatus", mock.Anything).Run(func(args mock.Arguments) {
		panic("first panic")
	}).Once()

	bridge := newUIBridge(ctx, mRenderer, true, true, false, true, "test.log", logger)

	// Send two events
	bridge.handleEvent(ctx, events.TurnStatusEvent{})
	bridge.handleEvent(ctx, events.ResponseEvent{})

	// Wait for shutdown and cleanup
	bridge.Cleanup()

	output := logBuffer.String()
	// Should contain the first panic
	assert.Contains(t, output, "uiBridge actor recovered from panic")
	assert.Contains(t, output, "first panic")
	// Should contain the skipping message
	assert.Contains(t, output, "Skipping remaining events during drain due to prior panic")

	// Verify that RenderResponse was NOT called (it was the second event in the queue)
	mRenderer.AssertNotCalled(t, "RenderResponse", mock.Anything, mock.Anything, mock.Anything)
}
