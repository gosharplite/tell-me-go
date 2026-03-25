// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// safeBuffer is a thread-safe wrapper around bytes.Buffer for testing concurrent I/O.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *safeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Reset()
}

// ControlledTicker allows us to trigger ticks manually for spinner frames.
type ControlledTicker struct {
	CChan <-chan time.Time
}

func (ct ControlledTicker) C() <-chan time.Time { return ct.CChan }
func (ct ControlledTicker) Stop()               {}

// controlledClock allows us to trigger ticks manually for spinner frames.
type controlledClock struct {
	mu          sync.RWMutex
	now         time.Time
	tickChannel chan time.Time
}

func (c *controlledClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *controlledClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *controlledClock) After(d time.Duration) <-chan time.Time {
	return c.tickChannel
}

func (c *controlledClock) NewTicker(d time.Duration) clock.Ticker {
	return ControlledTicker{CChan: c.tickChannel}
}

func (c *controlledClock) Jitter(base float64) float64 { return base }

func (c *controlledClock) Tick() {
	c.mu.Lock()
	c.now = c.now.Add(200 * time.Millisecond)
	c.mu.Unlock()
	c.tickChannel <- c.Now()
}

func TestSpinner_E2E_Visibility(t *testing.T) {
	// 1. Setup Environment
	var stdout, stderr safeBuffer
	clock := &controlledClock{
		now:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		tickChannel: make(chan time.Time, 1),
	}

	// Use the real UIRenderer from internal/ui
	uiRenderer := ui.NewRenderer(nil, &stdout, &stderr, clock)
	uiRenderer.SetForceSpinner(true) // Bypass TTY check for testing

	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background())
	defer func() { _ = mEventBus.Shutdown(context.Background()) }()

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	orch := newOrchestrator("home", "1.0.0", nil, nil, &stdout, &stderr, factory, nil, uiRenderer)

	// 2. Mock Agent Behavior
	// When Chat is called, it will emit events via the event bus.
	// Since we are testing the Orchestrator's wiring, we need to capture the bridge's handleEvent function.
	var capturedHandler func(context.Context, events.Event)
	mChatter.On("Subscribe", mock.Anything).Run(func(args mock.Arguments) {
		capturedHandler = args.Get(0).(func(context.Context, events.Event))
	}).Return()

	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
	mChatter.On("Shutdown", mock.Anything).Return(nil)
	mCapturer.On("IsTTY", mock.Anything).Return(true)

	// Simulate a "Thinking" process during Chat
	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Run(func(args mock.Arguments) {
		ctx := args.Get(0).(context.Context)

		// Phase A: Inference Starts
		capturedHandler(ctx, events.InferenceStartedEvent{})

		// Wait for the goroutine to draw first frame
		time.Sleep(50 * time.Millisecond)

		// Phase B: Ticks (Spinner frames)
		clock.Tick() // Frame 1
		time.Sleep(50 * time.Millisecond)
		clock.Tick() // Frame 2
		time.Sleep(50 * time.Millisecond)

		// Phase C: Response arrives
		capturedHandler(ctx, events.ResponseEvent{
			Content: &llm.Content{Parts: []*llm.Part{{Text: "The Answer"}}},
		})
	}).Return(nil)

	// 3. Execution
	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model:            "model",
		Mode:             "mode",
		SelectedProvider: "provider",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default())

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	assert.NoError(t, err)

	// 4. Assertions on Stderr
	output := stderr.String()

	// Check for Thinking message
	assert.Contains(t, output, "Thinking...", "Spinner message not found in stderr")

	// Check for at least two spinner frames (⠋, ⠙, ⠹, etc.)
	// Based on ui/renderer.go: frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	assert.Contains(t, output, "⠋", "First spinner frame not found")
	assert.Contains(t, output, "⠙", "Second spinner frame not found")

	// Check that the spinner was cleared (ANSI escape for clear line \033[2K)
	assert.Contains(t, output, "\033[2K", "Spinner was never cleared from stderr")

	// Verify timing message in the thinking line (0s, 1s, etc.)
	assert.Contains(t, output, "(0s)", "Initial timing message (0s) missing")
}

func TestSpinner_ContextTimeout_Resilience(t *testing.T) {
	// This test ensures that if the event bus handler times out (5s),
	// the spinner continues to run because it's using the bridge's session context.

	var stdout, stderr safeBuffer
	clock := &controlledClock{
		now:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		tickChannel: make(chan time.Time, 1),
	}

	uiRenderer := ui.NewRenderer(nil, &stdout, &stderr, clock)
	uiRenderer.SetForceSpinner(true)

	// Create bridge with a long-lived context
	sessionCtx := context.Background()
	bridge := newUIBridge(sessionCtx, uiRenderer, true, true, false, true, "log.txt")

	// Simulate InferenceStartedEvent arriving via a short-lived handler context
	handlerCtx, cancel := context.WithTimeout(sessionCtx, 100*time.Millisecond)
	defer cancel()

	bridge.handleEvent(handlerCtx, events.InferenceStartedEvent{})

	// Wait for handler context to expire
	time.Sleep(200 * time.Millisecond)

	// Trigger ticks - if the spinner is still alive, these will succeed
	stderr.Reset()
	clock.Tick()
	time.Sleep(50 * time.Millisecond)
	clock.Tick()
	time.Sleep(50 * time.Millisecond)

	output := stderr.String()
	assert.Contains(t, output, "⠙", "Spinner should still be ticking even after handler context expired")

	// Cleanup
	bridge.handleEvent(sessionCtx, events.ResponseEvent{Content: &llm.Content{}})
}
