// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// spinnerCoord manages the spinner lifecycle: starting, stopping, and resuming
// the terminal progress indicator based on domain events.
//
// All methods are confined to the actor goroutine; no mutexes are needed.
type spinnerCoord struct {
	renderer    ports.UIRenderer
	logger      ports.Logger
	activePhase events.Event
	stopFn      func()
}

// newSpinnerCoord creates a new spinnerCoord with the given dependencies.
func newSpinnerCoord(renderer ports.UIRenderer, logger ports.Logger) *spinnerCoord {
	return &spinnerCoord{
		renderer: renderer,
		logger:   logger,
	}
}

// stopActiveSpinner stops the currently running spinner, if any.
// It is safe to call even when no spinner is active.
func (sc *spinnerCoord) stopActiveSpinner() {
	stop := sc.stopFn
	sc.stopFn = nil

	if stop != nil {
		// Protect the boundary against double-panics from external UI dependencies
		func() {
			defer func() {
				if r := recover(); r != nil {
					sc.logger.Debug("Recovered from panic while stopping spinner", "panic", r)
				}
			}()
			stop()
		}()
	}
}

// resumeActiveSpinner restarts the spinner for the currently active phase, if
// any. It returns whether the spinner was started.
//
// resetRendering is an optional callback invoked before spinner transition when
// the current state is stateRendering and the active phase requires a reset
// (e.g. SummarizationStartedEvent). Pass nil when the caller knows the state
// cannot be stateRendering.
func (sc *spinnerCoord) resumeActiveSpinner(ctx context.Context, state uiState, resetRendering func() uiState) bool {
	phase := sc.activePhase
	if phase != nil {
		return sc.startSpinnerForPhase(ctx, phase, state, resetRendering)
	}
	return false
}

// startSpinnerForPhase starts the appropriate spinner for the given event.
// It returns whether the spinner was started.
//
// resetRendering is an optional callback invoked before spinner transition when
// the current state is stateRendering and the event requires a reset (e.g.
// SummarizationStartedEvent, RetryWaitingEvent). The callback must return the
// new state (typically stateIdle). Pass nil when the caller knows the state
// cannot be stateRendering.
func (sc *spinnerCoord) startSpinnerForPhase(ctx context.Context, e events.Event, state uiState, resetRendering func() uiState) bool {
	info, ok := getSpinnerInfo(e)
	if !ok {
		return false
	}

	// If the event requires a rendering reset and we are currently rendering,
	// invoke the callback to transition to idle before starting the spinner.
	if info.resetRendering && state == stateRendering && resetRendering != nil {
		state = resetRendering()
	}

	return sc.transitionSpinner(state, func() func() {
		if info.withMetrics {
			return sc.renderer.StartSpinnerWithMetrics(ctx, info.status)
		}
		return sc.renderer.StartSpinnerWithStatus(ctx, info.status)
	})
}

// transitionSpinner stops any active spinner and starts a new one using
// startFn. It returns false if the current state prohibits spinner changes
// (stateRendering or stateAwaitingConsent). The caller is responsible for
// updating the UI state to stateThinking on true.
func (sc *spinnerCoord) transitionSpinner(state uiState, startFn func() func()) bool {
	if state == stateRendering || state == stateAwaitingConsent {
		return false
	}

	sc.stopActiveSpinner()
	sc.stopFn = startFn()
	return true
}
