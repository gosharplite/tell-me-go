// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
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
