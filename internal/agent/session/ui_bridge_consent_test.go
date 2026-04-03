// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/mock"
)

func TestUIBridge_ConsentSpinnerLeak(t *testing.T) {
	t.Parallel()
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

	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Queue events in order: Start Consent -> Block Loop -> Trigger Suppressed Event -> Finish Consent
	_ = bridge.handleEvent(context.Background(), events.ConsentStartedEvent{})
	_ = bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "BLOCK"})
	_ = bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})
	_ = bridge.handleEvent(context.Background(), events.ConsentFinishedEvent{})

	// 2. Unblock the loop and ensure all queued events are processed
	close(block)
	syncBridge(t, bridge, mRenderer)

	// 3. Assert it was eventually resumed. .Maybe() recorded it, so AssertCalled will find it.
	mRenderer.AssertCalled(t, "StartSpinnerWithStatus", mock.Anything, " Compressing context...")
	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SystemMessageDuringConsent(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Start consent
	_ = bridge.handleEvent(context.Background(), events.ConsentStartedEvent{})
	syncBridge(t, bridge, mRenderer)

	// 2. System message arrives during consent
	mRenderer.On("LogSystemMessage", mock.Anything, "Hello", "info").Return().Once()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()
	_ = bridge.handleEvent(context.Background(), events.SystemMessageEvent{Message: "Hello", Level: "info"})
	syncBridge(t, bridge, mRenderer)

	// Should NOT start a spinner because isWaitingForConsent is true
	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SpinnerConsentCollision(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
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

	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogSystemMessage", mock.Anything, mock.MatchedBy(func(s string) bool {
		return s != "SYNC_SENTINEL"
	}), mock.Anything).Return().Maybe()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(startSpinner).Maybe()

	mRenderer.Test(t)

	ctx := context.Background()

	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(ctx)
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
		_ = bridge.handleEvent(ctx, events.ConsentStartedEvent{})

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
		_ = bridge.handleEvent(ctx, events.ConsentFinishedEvent{})
		syncBridge(t, bridge, mRenderer)
	}()

	go func() {
		defer wg.Done()
		// 1. Wait for consent to become active
		<-consentActiveCh

		// 2. This event triggers transitionSpinner internally
		_ = bridge.handleEvent(ctx, events.InferenceStartedEvent{Model: "gpt-4"})
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
