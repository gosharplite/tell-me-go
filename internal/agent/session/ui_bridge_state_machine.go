// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

// uiState represents the possible states of the UI bridge.
type uiState int

const (
	// stateIdle indicates the UI is not performing any active task.
	stateIdle uiState = iota
	// stateThinking indicates the UI is showing a progress indicator (spinner).
	stateThinking
	// stateRendering indicates the UI is rendering a streaming response.
	stateRendering
	// stateAwaitingConsent indicates the UI is waiting for user consent.
	stateAwaitingConsent
)

// uiStateMachine manages UI state transitions and queries.
// All state mutation is confined to the actor goroutine; no mutexes are needed.
type uiStateMachine struct {
	state   uiState
	spinner *spinnerCoord
}

// newUIStateMachine creates a new uiStateMachine with the given spinner
// dependency for side-effect delegation during transitions.
func newUIStateMachine(spinner *spinnerCoord) *uiStateMachine {
	return &uiStateMachine{
		state:   stateIdle,
		spinner: spinner,
	}
}

// transition changes the UI state to next, with appropriate spinner side
// effects based on the target state.
func (sm *uiStateMachine) transition(next uiState) {
	if sm.state == next {
		return
	}

	// Side effects for entering the new state
	switch next {
	case stateIdle, stateRendering, stateAwaitingConsent:
		sm.spinner.stopActiveSpinner()
	case stateThinking:
		// stateThinking side effects are typically handled via transitionSpinner
		// but we ensure old spinner is stopped if we were in another state.
		// If we were already in stateThinking, we don't stop here to avoid flicker
		// unless transitionSpinner is called.
		if sm.state != stateThinking {
			sm.spinner.stopActiveSpinner()
		}
	}

	sm.state = next
}

// is reports whether the current state equals s.
func (sm *uiStateMachine) is(s uiState) bool {
	return sm.state == s
}

// current returns the current state. Used where callers need to read the state
// for passing to spinnerCoord methods.
func (sm *uiStateMachine) current() uiState {
	return sm.state
}

// setState directly sets the state without side effects. Used ONLY for
// stateThinking after spinnerCoord has already started the spinner.
// All other state changes must go through transition().
func (sm *uiStateMachine) setState(s uiState) {
	sm.state = s
}
