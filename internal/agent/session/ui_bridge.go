// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// bridgeOption configures a uiBridge instance.
type bridgeOption func(*uiBridge)

// withBridgeThoughts enables or disables thought rendering.
func withBridgeThoughts(show bool) bridgeOption {
	return func(b *uiBridge) { b.showThoughts = show }
}

// withBridgeTools enables or disables tool call rendering.
func withBridgeTools(show bool) bridgeOption {
	return func(b *uiBridge) { b.showTools = show }
}

// withBridgeRawOutput enables or disables raw output mode.
func withBridgeRawOutput(raw bool) bridgeOption {
	return func(b *uiBridge) { b.rawOutput = raw }
}

// withBridgeColor enables or disables ANSI color support.
func withBridgeColor(color bool) bridgeOption {
	return func(b *uiBridge) { b.useColor = color }
}

// withBridgeLogFile sets the file path for logging usage metrics.
func withBridgeLogFile(path string) bridgeOption {
	return func(b *uiBridge) { b.logFile = path }
}

// withBridgeLogger sets the structured logger.
func withBridgeLogger(l ports.Logger) bridgeOption {
	return func(b *uiBridge) { b.logger = l }
}

// withBridgeClock sets the clock for deterministic timestamps.
func withBridgeClock(c clock.Clock) bridgeOption {
	return func(b *uiBridge) { b.clock = c }
}

// withBridgeCleanupTimeout sets the duration to wait for the bridge to drain events during cleanup.
func withBridgeCleanupTimeout(d time.Duration) bridgeOption {
	return func(b *uiBridge) { b.cleanupTimeout = d }
}

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

// uiBridge translates domain events into UI updates.
type uiBridge struct {
	loopCtx            context.Context
	loopCancel         context.CancelFunc
	cancel             context.CancelFunc
	renderer           ports.UIRenderer
	logger             ports.Logger
	clock              clock.Clock
	showThoughts       bool
	showTools          bool
	rawOutput          bool
	useColor           bool
	logFile            string
	state              uiState
	stopSpinner        func()
	activePhase        events.Event
	eventCh            chan events.Event
	closeOnce          sync.Once
	cleanupOnce        sync.Once
	cleanupInvocations int32
	wg                 sync.WaitGroup
	cleanupTimeout     time.Duration
	isClosed           atomic.Bool
	started            chan struct{}
	startOnce          sync.Once
}

// newUIBridge creates a new uiBridge.
func newUIBridge(renderer ports.UIRenderer, opts ...bridgeOption) *uiBridge {
	loopCtx, loopCancel := context.WithCancel(context.Background())
	b := &uiBridge{
		loopCtx:        loopCtx,
		loopCancel:     loopCancel,
		renderer:       renderer,
		logger:         &ports.NoOpLogger{},
		clock:          clock.RealClock{},
		eventCh:        make(chan events.Event, 100),
		cleanupTimeout: 5 * time.Second,
		started:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.logger == nil {
		b.logger = &ports.NoOpLogger{}
	}
	b.wg.Add(1)
	return b
}
func (b *uiBridge) transition(next uiState) {
	if b.state == next {
		return
	}

	// Side effects for entering the new state
	switch next {
	case stateIdle, stateRendering, stateAwaitingConsent:
		b.stopActiveSpinner()
	case stateThinking:
		// stateThinking side effects are typically handled via transitionSpinner
		// but we ensure old spinner is stopped if we were in another state.
		// If we were already in stateThinking, we don't stop here to avoid flicker
		// unless transitionSpinner is called.
		if b.state != stateThinking {
			b.stopActiveSpinner()
		}
	}

	b.state = next
}
func (b *uiBridge) is(state uiState) bool {
	return b.state == state
}
func (b *uiBridge) stopActiveSpinner() {
	stop := b.stopSpinner
	b.stopSpinner = nil

	if stop != nil {
		// Protect the boundary against double-panics from external UI dependencies
		func() {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Debug("Recovered from panic while stopping spinner", "panic", r)
				}
			}()
			stop()
		}()
	}
}
func (b *uiBridge) resumeActiveSpinner(ctx context.Context) {
	phase := b.activePhase
	if phase != nil {
		b.startSpinnerForPhase(ctx, phase)
	}
}
func (b *uiBridge) CloseInput() {
	b.closeOnce.Do(func() {
		b.isClosed.Store(true)
		close(b.eventCh)
	})
}
func (b *uiBridge) Cleanup() {
	b.cleanupOnce.Do(func() {
		atomic.AddInt32(&b.cleanupInvocations, 1)

		// 1. Set up the wait mechanism
		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					b.logger.Error("panic in UI bridge cleanup wait", "error", r, "stack", string(debug.Stack()))
					close(done)
				}
			}()
			b.wg.Wait()
			close(done)
		}()

		// 2. Wait with timeout
		timer := time.NewTimer(b.cleanupTimeout)
		defer timer.Stop()

		select {
		case <-done:
			// Clean exit: all workers finished draining within the timeout
		case <-timer.C:
			// Timeout reached: The renderer might be deadlocked or too slow.
			b.logger.Warn("UI Bridge cleanup timed out, forcing context cancellation")
		}

		// Ensure the context is cancelled to unblock the Listen loop.
		if b.cancel != nil {
			b.cancel()
		}
	})
}
func (b *uiBridge) Listen(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	defer cancel()

	defer b.loopCancel()
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("panic in uiBridge loop", "error", r, "stack", string(debug.Stack()))
			b.stopActiveSpinner()
			err = fmt.Errorf("uiBridge panicked: %v", r)
		}
	}()
	defer b.wg.Done()

	b.startOnce.Do(func() {
		close(b.started)
	})

	for {
		select {
		case <-ctx.Done():
			// Forced abort
			b.stopActiveSpinner()
			return nil
		case e, ok := <-b.eventCh:
			if !ok {
				b.stopActiveSpinner()
				return nil
			}
			b.processRecoverable(ctx, e)
		}
	}
}
func (b *uiBridge) processRecoverable(ctx context.Context, e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("uiBridge actor recovered from panic", "error", r)
			b.logger.Debug("uiBridge recovery stack trace", "stack", string(debug.Stack()))
			b.stopActiveSpinner()
			panic(r) // Re-panic to stop the Listen loop
		}
	}()
	b.processEvent(ctx, e)
}
func (b *uiBridge) handleEvent(ctx context.Context, e events.Event) error {
	if b.isClosed.Load() {
		b.logger.Debug("Shedding event: bridge is closed")
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			// Log as Warn/Error since it's now unexpected (last-resort safety)
			b.logger.Warn("Unexpected panic in uiBridge.handleEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return b.enqueueEvent(ctx, e)
}
func (b *uiBridge) enqueueEvent(ctx context.Context, e events.Event) error {
	if isCriticalEvent(e) {
		// Critical events: ensure delivery and enforce true backpressure.
		select {
		case b.eventCh <- e:
			return nil
		case <-ctx.Done():
			b.logger.Debug("Caller context cancelled while waiting to queue critical event")
			return ctx.Err()
		case <-b.loopCtx.Done(): // NEW: Consumer liveness check
			return fmt.Errorf("uiBridge actor is dead: %w", b.loopCtx.Err())
		}
	}

	// Safe to shed visual/transient events if queue is full
	select {
	case b.eventCh <- e:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-b.loopCtx.Done(): // NEW: Consumer liveness check
		return fmt.Errorf("uiBridge actor is dead: %w", b.loopCtx.Err())
	default:
		b.logger.Debug("UI Bridge queue full, shedding load/visual event")
		return nil
	}
}
func (b *uiBridge) processEvent(ctx context.Context, e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		b.handleTurnStatus(ctx, ev)
	case events.InferenceStartedEvent, events.SummarizationStartedEvent, events.ToolExecutionStartedEvent, events.RetryWaitingEvent:
		b.handleSpinnerEvent(ctx, ev)
	case events.ConsentStartedEvent:
		b.transition(stateAwaitingConsent)
	case events.ConsentFinishedEvent:
		// Transition back to Idle, which stops any lingering (though should be stopped by ConsentStarted)
		// resumeActiveSpinner will transition to stateThinking if a phase exists.
		b.transition(stateIdle)
		b.resumeActiveSpinner(ctx)
	case events.ResponseEvent:
		b.handleResponse(ctx, ev)
	case events.UsageMetricsEvent:
		b.handleUsageMetrics(ev)
	case events.ToolCallEvent, events.ToolResultEvent:
		b.handleToolEvents(ctx, ev)
	case events.TurnStarted:
		b.handleTurnStarted()
	case events.SystemMessageEvent, events.StatusUpdate:
		b.handleSystemMessage(ctx, ev)
	}
}
func (b *uiBridge) handleSystemMessage(ctx context.Context, e events.Event) {
	var msg, lvl string

	switch ev := e.(type) {
	case events.SystemMessageEvent:
		msg, lvl = ev.Message, ev.Level
	case events.StatusUpdate:
		msg, lvl = ev.Message, ev.Level
	default:
		return
	}
	b.stopActiveSpinner()
	b.renderer.LogSystemMessage(ctx, msg, lvl)
	b.resumeActiveSpinner(ctx)
}
func (b *uiBridge) handleSpinnerEvent(ctx context.Context, e events.Event) {
	b.activePhase = e
	b.startSpinnerForPhase(ctx, e)
}
func (b *uiBridge) startSpinnerForPhase(ctx context.Context, e events.Event) {
	info, ok := getSpinnerInfo(e)
	if !ok {
		return
	}

	if info.resetRendering && b.is(stateRendering) {
		b.transition(stateIdle)
	}

	b.transitionSpinner(func() func() {
		if info.withMetrics {
			return b.renderer.StartSpinnerWithMetrics(ctx, info.status)
		}
		return b.renderer.StartSpinnerWithStatus(ctx, info.status)
	})
}
func (b *uiBridge) handleTurnStatus(ctx context.Context, ev events.TurnStatusEvent) {
	b.activePhase = nil // Clear phase on new turn/header
	b.transition(stateIdle)
	b.renderer.LogTurnStatus(ctx, ev.Status)
}
func (b *uiBridge) handleResponse(ctx context.Context, ev events.ResponseEvent) {
	b.activePhase = nil // Clear phase on response
	b.transition(stateRendering)
	b.renderer.RenderResponse(ctx, ev.Content, b.showThoughts, b.rawOutput)
}
func (b *uiBridge) handleUsageMetrics(ev events.UsageMetricsEvent) {
	ctx := b.ensureContext(ev.Context, "UsageMetricsEvent")
	b.stopActiveSpinner()
	b.renderer.LogUsage(ctx, ev.Metrics, b.logFile, ev.StartTime)
	b.resumeActiveSpinner(ctx)
}
func (b *uiBridge) handleToolEvents(ctx context.Context, e events.Event) {
	switch ev := e.(type) {
	case events.ToolCallEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolCall(ctx, ev.Calls, ev.Turn, ev.MaxTurns, b.showTools)
		b.resumeActiveSpinner(ctx)
	case events.ToolResultEvent:
		b.stopActiveSpinner()
		b.renderer.LogToolResult(ctx, ev.Name, ev.Result, b.showTools)
		b.resumeActiveSpinner(ctx)
	}
}
func (b *uiBridge) handleTurnStarted() {
	b.activePhase = nil
	b.transition(stateIdle)
}
func (b *uiBridge) transitionSpinner(startFn func() func()) {
	if b.state == stateRendering || b.state == stateAwaitingConsent {
		return
	}

	b.stopActiveSpinner()
	b.stopSpinner = startFn()
	b.state = stateThinking
}
func (b *uiBridge) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		b.logger.Debug(name + " missing context")
		return context.Background()
	}
	return ctx
}

type spinnerInfo struct {
	status         string
	withMetrics    bool
	resetRendering bool
}

func isCriticalEvent(e events.Event) bool {
	switch e.(type) {
	case events.ResponseEvent, events.SystemMessageEvent, events.StatusUpdate,
		events.ConsentStartedEvent, events.ConsentFinishedEvent,
		events.TurnStarted, events.TurnStatusEvent,
		events.ToolCallEvent, events.ToolResultEvent,
		events.UsageMetricsEvent:
		return true
	default:
		return false
	}
}
func getSpinnerInfo(e events.Event) (spinnerInfo, bool) {
	switch ev := e.(type) {
	case events.InferenceStartedEvent:
		status := " Thinking..."
		if ev.Model != "" {
			status = fmt.Sprintf(" Thinking [%s]...", ev.Model)
		}
		return spinnerInfo{status: status, withMetrics: false, resetRendering: false}, true
	case events.SummarizationStartedEvent:
		return spinnerInfo{status: " Compressing context...", withMetrics: false, resetRendering: true}, true
	case events.ToolExecutionStartedEvent:
		status := " Executing tools..."
		if len(ev.ToolNames) == 1 {
			status = fmt.Sprintf(" Executing [%s]...", ev.ToolNames[0])
		} else if len(ev.ToolNames) > 1 {
			status = fmt.Sprintf(" Executing tools [%s]...", strings.Join(ev.ToolNames, ", "))
		}
		return spinnerInfo{status: status, withMetrics: true, resetRendering: true}, true
	case events.RetryWaitingEvent:
		return spinnerInfo{status: fmt.Sprintf(" Retrying in %v...", ev.Duration.Round(time.Second)), withMetrics: false, resetRendering: true}, true
	default:
		return spinnerInfo{}, false
	}
}
func (b *uiBridge) GetLoopContext() context.Context {
	return b.loopCtx
}

func (b *uiBridge) WaitStarted() {
	<-b.started
}
