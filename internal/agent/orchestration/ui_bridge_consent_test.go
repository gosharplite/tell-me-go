// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestUIBridge_ConsentSpinnerLeak(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")

	// 1. Start consent
	bridge.handleEvent(context.Background(), events.ConsentStartedEvent{})

	bridge.mu.Lock()
	assert.True(t, bridge.isWaitingForConsent, "Expected isWaitingForConsent to be true")
	bridge.mu.Unlock()

	// 2. Trigger a spinner event during consent - should be suppressed
	// We don't expect StartSpinnerWithStatus to be called yet
	bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})

	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)

	// 3. Finish consent - should resume the suppressed spinner
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Return(func() {}).Once()
	bridge.handleEvent(context.Background(), events.ConsentFinishedEvent{})

	bridge.mu.Lock()
	assert.False(t, bridge.isWaitingForConsent, "Expected isWaitingForConsent to be false")
	bridge.mu.Unlock()

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SystemMessageDuringConsent(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")

	// 1. Start consent
	bridge.handleEvent(context.Background(), events.ConsentStartedEvent{})

	// 2. System message arrives during consent
	mRenderer.On("LogSystemMessage", "Hello", "info").Return().Once()
	bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "Hello", Level: "info"})

	// Should NOT start a spinner because isWaitingForConsent is true
	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SpinnerConsentCollision(t *testing.T) {
	m := &collisionMock{}
	m.On("LogTurnStatus", mock.Anything).Return().Maybe()
	m.On("LogSystemMessage", mock.Anything, mock.Anything).Return().Maybe()
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
	
	bridge := newUIBridge(context.Background(), &mockCollisionRenderer{collisionMock: m, startFn: startSpinner}, true, true, false, true, "log.txt")
	ctx := context.Background()

	// High-iteration loop to hammer the race window
	for i := 0; i < 5000; i++ {
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

func (m *mockCollisionRenderer) RenderResponse(content *llm.Content, showThoughts, rawOutput bool) {
	m.Called(content, showThoughts, rawOutput)
}

func (m *mockCollisionRenderer) LogTurnStatus(status events.TurnStatus) {
	m.Called(status)
}

func (m *mockCollisionRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *mockCollisionRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.Called(calls, turn, maxTurns, showTools)
}

func (m *mockCollisionRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	m.Called(name, result, showTools)
}

func (m *mockCollisionRenderer) LogSystemMessage(msg string, level string) {
	m.Called(msg, level)
}

func (m *mockCollisionRenderer) SetUseColor(use bool) {
	m.Called(use)
}

func (m *mockCollisionRenderer) SetForceSpinner(force bool) {
	m.Called(force)
}
