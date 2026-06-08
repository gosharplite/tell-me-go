// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIBridge_ConsentSpinnerLeak(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	block := make(chan struct{})

	// Track spinner statuses for later assertion
	var spinnerStatuses []string
	var mu sync.Mutex

	// Setup a block to freeze the actor loop
	mRenderer.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {
		if msg == "BLOCK" {
			<-block
		}
	}

	// Handle spinner calls
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		mu.Lock()
		spinnerStatuses = append(spinnerStatuses, status)
		mu.Unlock()
		return func() {}
	}

	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Queue events in order: Start Consent -> Block Loop -> Trigger Suppressed Event -> Finish Consent
	_ = bridge.HandleEvent(context.Background(), events.ConsentStartedEvent{})
	_ = bridge.HandleEvent(context.Background(), events.SystemMessageEvent{Message: "BLOCK"})
	_ = bridge.HandleEvent(context.Background(), events.SummarizationStartedEvent{})
	_ = bridge.HandleEvent(context.Background(), events.ConsentFinishedEvent{})

	// 2. Unblock the loop and ensure all queued events are processed
	close(block)
	syncBridge(t, bridge, mRenderer)

	// 3. Assert it was eventually resumed.
	found := false
	mu.Lock()
	for _, s := range spinnerStatuses {
		if s == " Compressing context..." {
			found = true
			break
		}
	}
	mu.Unlock()
	assert.True(t, found, "expected StartSpinnerWithStatus to be called with ' Compressing context...'")
}

func TestUIBridge_SystemMessageDuringConsent(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Start consent
	_ = bridge.HandleEvent(context.Background(), events.ConsentStartedEvent{})
	syncBridge(t, bridge, mRenderer)

	// 2. System message arrives during consent
	systemMsgCalled := make(chan struct{})
	mRenderer.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {
		if msg == "Hello" {
			close(systemMsgCalled)
		}
	}
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() { return func() {} }
	_ = bridge.HandleEvent(context.Background(), events.SystemMessageEvent{Message: "Hello", Level: "info"})
	syncBridge(t, bridge, mRenderer)

	// Verify the system message was processed
	select {
	case <-systemMsgCalled:
	default:
		t.Error("expected LogSystemMessage to be called for 'Hello'")
	}

	// Should NOT start a spinner because isWaitingForConsent is true
	snap := mRenderer.Snapshot()
	if snap.StartSpinnerWithStatus > 0 {
		t.Error("StartSpinnerWithStatus should not have been called during consent")
	}
}

func TestUIBridge_SpinnerConsentCollision(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	var mu sync.Mutex
	spinnerRunning := false
	consentActive := false
	overlap := false

	// Helper to update spinner state
	startSpinner := func() func() {
		mu.Lock()
		spinnerRunning = true
		mu.Unlock()
		return func() {
			mu.Lock()
			spinnerRunning = false
			mu.Unlock()
		}
	}

	mRenderer.LogTurnStatusFn = func(ctx context.Context, status events.TurnStatus) {}
	mRenderer.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {}
	mRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		return startSpinner()
	}

	testCtx := context.Background()

	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	consentActiveCh := make(chan struct{})
	inferenceDoneCh := make(chan struct{})

	go func() {
		defer wg.Done()
		// 1. Simulation of consent cycle
		_ = bridge.HandleEvent(testCtx, events.ConsentStartedEvent{})

		// 2. Block until the asynchronous worker signals it has processed the state
		syncBridge(t, bridge, mRenderer)

		mu.Lock()
		consentActive = true
		mu.Unlock()

		// 3. Signal inference goroutine that consent is active
		close(consentActiveCh)

		// 4. Wait for inference goroutine to process its event
		<-inferenceDoneCh

		mu.Lock()
		consentActive = false
		mu.Unlock()
		_ = bridge.HandleEvent(testCtx, events.ConsentFinishedEvent{})
		syncBridge(t, bridge, mRenderer)
	}()

	go func() {
		defer wg.Done()
		// 1. Wait for consent to become active
		<-consentActiveCh

		// 2. This event triggers transitionSpinner internally
		_ = bridge.HandleEvent(testCtx, events.InferenceStartedEvent{Model: "gpt-4"})
		syncBridge(t, bridge, mRenderer)

		// 3. If transitionSpinner returns and the spinner is STILL running while consent is active, we have an overlap
		mu.Lock()
		if spinnerRunning && consentActive {
			overlap = true
		}
		mu.Unlock()

		// 4. Signal consent goroutine to finish
		close(inferenceDoneCh)
	}()

	wg.Wait()

	mu.Lock()
	if overlap {
		mu.Unlock()
		t.Fatalf("Spinner overlapped with Consent Prompt!")
	}
	mu.Unlock()
}

func TestUIBridge_DeadConsumer_Unblocks(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer)

	// Start the bridge to initialize everything
	_, cancel, _ := startListen(t, bridge)
	bridge.WaitStarted()
	bridge.WaitStarted()

	// Simulate the consumer dying unexpectedly (e.g., panic or external cancellation)
	// Canceling the parent loop context forces the loop to exit and triggers defer bridge.loopCancel()
	cancel()

	// Wait for the consumer loop to exit to ensure it's truly dead and not reading
	bridge.wg.Wait()

	// Fill the bridge's event channel buffer to its capacity (100)
	// This ensures that the enqueue blocks deterministically,
	// forcing the select block to rely on <-loopCtx.Done()
	for i := 0; i < 100; i++ {
		// Bypass the enqueue method to strictly fill the channel
		bridge.queue.(*eventQueue).sendDirect(events.TurnStarted{})
	}

	// Attempt to send a critical event that requires delivery (e.g., ConsentStartedEvent)
	err := bridge.HandleEvent(context.Background(), events.ConsentStartedEvent{})

	// Assert it immediately unblocks and returns the specific liveness check error
	require.Error(t, err, "Expected HandleEvent to fail because the bridge is dead")
	assert.Contains(t, err.Error(), "uibridge actor is dead")
}
