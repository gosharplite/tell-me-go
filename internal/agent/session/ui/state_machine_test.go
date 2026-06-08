// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
)

// --- helpers ---

// sentinelStopFn returns a func that toggles *called and a helper to read it.
// Use this to detect whether stopActiveSpinner invokes the stored stopFn.
func sentinelStopFn() (stopFn func(), wasCalled func() bool) {
	called := false
	return func() { called = true }, func() bool { return called }
}

func newTestStateMachine(t *testing.T) (*uiStateMachine, *spinnerCoord) {
	t.Helper()
	renderer := &spyRenderer{}
	logger := &testfixtures.SpyLogger{}
	sc := newSpinnerCoord(renderer, logger)
	sm := newUIStateMachine(sc)
	return sm, sc
}

// --- tests ---

func TestTransition_SelfTransition_NoOp(t *testing.T) {
	t.Parallel()

	sm, sc := newTestStateMachine(t)

	// Set up: state is thinking, spinner is active with a sentinel stopFn.
	sm.state = stateThinking
	stopFn, wasCalled := sentinelStopFn()
	sc.stopFn = stopFn

	// Act: transition to the same state.
	sm.transition(stateThinking)

	// Assert: state unchanged, spinner NOT stopped.
	assert.Equal(t, stateThinking, sm.current(), "state must remain stateThinking after self-transition")
	assert.False(t, wasCalled(), "stopFn must not be called during self-transition (guard at line 39-41)")
	assert.NotNil(t, sc.stopFn, "stopFn must not be cleared during self-transition")
}

func TestTransition_ToThinking_StopsSpinner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fromState uiState
	}{
		{"IdleToThinking", stateIdle},
		{"RenderingToThinking", stateRendering},
		{"AwaitingConsentToThinking", stateAwaitingConsent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm, sc := newTestStateMachine(t)

			// Set up: state is the fromState, spinner is active with a sentinel.
			sm.state = tt.fromState
			stopFn, wasCalled := sentinelStopFn()
			sc.stopFn = stopFn

			// Act: transition to stateThinking.
			sm.transition(stateThinking)

			// Assert: state changed, spinner was stopped.
			assert.Equal(t, stateThinking, sm.current(),
				"state must transition to stateThinking from %v", tt.fromState)
			assert.True(t, wasCalled(),
				"stopFn must be called when entering stateThinking from %v (line 53)", tt.fromState)
			assert.Nil(t, sc.stopFn,
				"stopFn must be cleared after stopActiveSpinner (line 53)")
		})
	}
}
