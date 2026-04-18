// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// controlledTicker allows us to trigger ticks manually for spinner frames.
type controlledTicker struct {
	CChan <-chan time.Time
}

func (ct controlledTicker) C() <-chan time.Time { return ct.CChan }
func (ct controlledTicker) Stop()               {}

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

func (c *controlledClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
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
	return controlledTicker{CChan: c.tickChannel}
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
	stdoutRaw, stderrRaw := testutil.NewSafeBuffer(), testutil.NewSafeBuffer()
	stderr := &testutil.SyncWriter{Writer: stderrRaw, OnWrite: make(chan struct{}, 100)}
	stdout := stdoutRaw
	clock := &controlledClock{
		now:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		tickChannel: make(chan time.Time, 1),
	}

	// Use the real UIRenderer from internal/ui
	uiRenderer := ui.NewRenderer(nil, stdout, stderr, clock, nil)
	uiRenderer.SetForceSpinner(true) // Bypass TTY check for testing

	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	orch := session.NewSessionManager("home", "1.0.0", nil, nil, stdout, stderr, factory, nil, uiRenderer, clock, strings.NewReader("deterministic_entropy"))

	// 2. Mock Agent Behavior
	// When Chat is called, it will emit events via the event bus.
	// Since we are testing the SessionManager's wiring, we need to capture the bridge's handleEvent function.
	var capturedHandler func(context.Context, events.Event)
	mChatter.On("Subscribe", mock.Anything).Run(func(args mock.Arguments) {
		sub := args.Get(0).(func(context.Context, events.Event))
		capturedHandler = sub
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

		// Wait for the spinner to start and write to stderr
		select {
		case <-stderr.OnWrite:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for spinner start")
		}
		assert.Contains(t, stderrRaw.String(), "Thinking...")

		// Phase B: Ticks (Spinner frames)
		clock.Tick() // Frame 1
		select {
		case <-stderr.OnWrite:
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for spinner frame 1")
		}

		clock.Tick() // Frame 2
		select {
		case <-stderr.OnWrite:
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for spinner frame 2")
		}

		// Phase C: Response arrives
		capturedHandler(ctx, events.ResponseEvent{
			Content: &llm.Content{Parts: []*llm.Part{{Text: "The Answer"}}},
		})
	}).Return(nil)

	// 3. Execution
	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model:            "model",
		Mode:             "mode",
		SelectedProvider: "provider",
	})
	deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	assert.NoError(t, err)

	// 4. Assertions on Stderr
	output := stderrRaw.String()

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

	stdoutRaw, stderrRaw := testutil.NewSafeBuffer(), testutil.NewSafeBuffer()
	stderr := &testutil.SyncWriter{Writer: stderrRaw, OnWrite: make(chan struct{}, 100)}
	stdout := stdoutRaw
	clock := &controlledClock{
		now:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		tickChannel: make(chan time.Time, 1),
	}

	uiRenderer := ui.NewRenderer(nil, stdout, stderr, clock, nil)
	uiRenderer.SetForceSpinner(true)

	// Create bridge with a long-lived context
	bridge := session.NewUIBridge(uiRenderer,
		session.WithBridgeThoughts(true),
		session.WithBridgeTools(true),
		session.WithBridgeRawOutput(false),
		session.WithBridgeColor(true),
		session.WithBridgeLogFile("log.txt"),
		session.WithBridgeLogger(slog.Default()),
	)
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	t.Cleanup(sessionCancel)
	errChan := make(chan error, 1)
	go func() {
		if err := bridge.Listen(sessionCtx); err != nil && !errors.Is(err, context.Canceled) {
			errChan <- err
		}
		close(errChan)
	}()
	bridge.WaitStarted()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// Simulate InferenceStartedEvent arriving via a short-lived handler context
	handlerCtx, cancel := context.WithTimeout(sessionCtx, 100*time.Millisecond)
	defer cancel()

	_ = bridge.HandleEvent(handlerCtx, events.InferenceStartedEvent{})

	// Wait for the spinner to start
	select {
	case <-stderr.OnWrite:
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for spinner start")
	}
	assert.Contains(t, stderrRaw.String(), "Thinking...")

	// Trigger ticks - if the spinner is still alive, these will succeed
	stderrRaw.Reset()
	clock.Tick()
	select {
	case <-stderr.OnWrite:
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for spinner tick after handler context expired")
	}
	clock.Tick()
	select {
	case <-stderr.OnWrite:
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for second spinner tick")
	}

	output := stderrRaw.String()
	assert.Contains(t, output, "⠙", "Spinner should still be ticking even after handler context expired")

	// Cleanup
	_ = bridge.HandleEvent(sessionCtx, events.ResponseEvent{Content: &llm.Content{}})
}
