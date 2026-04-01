// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

func TestUIBridge_ConsentSpinnerLeak(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	block := make(chan struct{})

	// Setup a block to freeze the actor loop
	mRenderer.On("LogSystemMessage", mock.Anything, "BLOCK", mock.Anything).Run(func(_ mock.Arguments) {
		<-block
	}).Return().Once()

	// Handle other expected calls with .Maybe() to prevent panic masking.
	// We use a specific matcher to avoid overlap with SYNC_SENTINEL used by syncBridge.
	mRenderer.On("LogSystemMessage", mock.Anything, mock.MatchedBy(func(s string) bool {
		return s != "BLOCK" && s != "SYNC_SENTINEL"
	}), mock.Anything).Return().Maybe()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()

	bridge := newUIBridge(context.Background(), mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Queue events in order: Start Consent -> Block Loop -> Trigger Suppressed Event -> Finish Consent
	bridge.handleEvent(context.Background(), events.ConsentStartedEvent{})
	bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "BLOCK"})
	bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})
	bridge.handleEvent(context.Background(), events.ConsentFinishedEvent{})

	// 2. Unblock the loop and ensure all queued events are processed
	close(block)
	syncBridge(t, bridge, mRenderer)

	// 3. Assert it was eventually resumed. .Maybe() recorded it, so AssertCalled will find it.
	mRenderer.AssertCalled(t, "StartSpinnerWithStatus", mock.Anything, " Compressing context...")
	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SystemMessageDuringConsent(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Start consent
	bridge.handleEvent(context.Background(), events.ConsentStartedEvent{})
	syncBridge(t, bridge, mRenderer)

	// 2. System message arrives during consent
	mRenderer.On("LogSystemMessage", mock.Anything, "Hello", "info").Return().Once()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()
	bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "Hello", Level: "info"})
	syncBridge(t, bridge, mRenderer)

	// Should NOT start a spinner because isWaitingForConsent is true
	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SpinnerConsentCollision(t *testing.T) {
	m := &collisionMock{}
	m.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
	m.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	m.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()

	// Helper to update spinner state
	startSpinner := func() func() {
		m.mu.Lock()
		m.spinnerRunning = true
		m.mu.Unlock()
		return func() {
			m.mu.Lock()
			m.spinnerRunning = false
			m.mu.Unlock()
		}
	}

	m.Test(t)

	ctx := context.Background()

	// High-iteration loop to hammer the race window
	for i := 0; i < 500; i++ {
		bridge := newUIBridge(context.Background(), &mockCollisionRenderer{collisionMock: m, startFn: startSpinner}, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			// Simulation of consent cycle
			bridge.handleEvent(ctx, events.ConsentStartedEvent{})
			m.mu.Lock()
			m.consentActive = true
			m.mu.Unlock()

			runtime.Gosched()

			m.mu.Lock()
			m.consentActive = false
			m.mu.Unlock()
			bridge.handleEvent(ctx, events.ConsentFinishedEvent{})
		}()

		go func() {
			defer wg.Done()
			// This event triggers transitionSpinner internally
			bridge.handleEvent(ctx, events.InferenceStartedEvent{Model: "gpt-4"})

			// If transitionSpinner returns and the spinner is STILL running while consent is active, we have an overlap
			m.mu.Lock()
			if m.spinnerRunning && m.consentActive {
				m.overlap = true
			}
			m.mu.Unlock()
		}()

		wg.Wait()

		m.mu.Lock()
		if m.overlap {
			m.mu.Unlock()
			t.Fatalf("Iteration %d: Spinner overlapped with Consent Prompt!", i)
		}
		m.mu.Unlock()

		bridge.CloseInput()
		bridge.Cleanup()
	}
}

type mockCollisionRenderer struct {
	*collisionMock
	startFn func() func()
}

type collisionMock struct {
	mock.Mock
	mu             sync.Mutex
	spinnerRunning bool
	consentActive  bool
	overlap        bool
}

func (m *mockCollisionRenderer) StartSpinner(ctx context.Context) func() {
	return m.startFn()
}

func (m *mockCollisionRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	return m.startFn()
}

func (m *mockCollisionRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	return m.startFn()
}

func (m *mockCollisionRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
	m.Called(ctx, content, showThoughts, rawOutput)
}

func (m *mockCollisionRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	m.Called(ctx, status)
}

func (m *mockCollisionRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *mockCollisionRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.Called(ctx, calls, turn, maxTurns, showTools)
}

func (m *mockCollisionRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
	m.Called(ctx, name, result, showTools)
}

func (m *mockCollisionRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.Called(ctx, msg, level)
}

func (m *mockCollisionRenderer) SetUseColor(use bool) {
	m.Called(use)
}

func (m *mockCollisionRenderer) SetForceSpinner(force bool) {
	m.Called(force)
}
