// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// bridgeOption configures a UIBridge instance.
type bridgeOption func(*UIBridge)

// WithBridgeThoughts enables or disables thought rendering.
func WithBridgeThoughts(show bool) bridgeOption {
	return func(b *UIBridge) { b.showThoughts = show }
}

// WithBridgeTools enables or disables tool call rendering.
func WithBridgeTools(show bool) bridgeOption {
	return func(b *UIBridge) { b.showTools = show }
}

// WithBridgeRawOutput enables or disables raw output mode.
func WithBridgeRawOutput(raw bool) bridgeOption {
	return func(b *UIBridge) { b.rawOutput = raw }
}

// WithBridgeColor enables or disables ANSI color support.
func WithBridgeColor(color bool) bridgeOption {
	return func(b *UIBridge) { b.useColor = color }
}

// WithBridgeLogFile sets the file path for logging usage metrics.
func WithBridgeLogFile(path string) bridgeOption {
	return func(b *UIBridge) { b.logFile = path }
}

// WithBridgeLogger sets the structured logger.
func WithBridgeLogger(l ports.Logger) bridgeOption {
	return func(b *UIBridge) { b.logger = l }
}

// withBridgeClock sets the clock for deterministic timestamps.
func withBridgeClock(c clock.Clock) bridgeOption {
	return func(b *UIBridge) { b.clock = c }
}

// withBridgeCleanupTimeout sets the duration to wait for the bridge to drain events during cleanup.
func withBridgeCleanupTimeout(d time.Duration) bridgeOption {
	return func(b *UIBridge) { b.cleanupTimeout = d }
}

// UIBridge translates domain events into UI updates.
type UIBridge struct {
	mu                 sync.RWMutex
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
	stateMachine       *uiStateMachine
	spinner            *spinnerCoord
	queue              *eventQueue
	dispatcher         *eventDispatcher
	cleanupOnce        sync.Once
	cleanupInvocations int32
	wg                 sync.WaitGroup
	cleanupTimeout     time.Duration
	started            chan struct{}
	startOnce          sync.Once
}

// NewUIBridge creates a new UIBridge.
func NewUIBridge(renderer ports.UIRenderer, opts ...bridgeOption) *UIBridge {
	loopCtx, loopCancel := context.WithCancel(context.Background())
	b := &UIBridge{
		loopCtx:        loopCtx,
		loopCancel:     loopCancel,
		renderer:       renderer,
		logger:         slog.Default(),
		clock:          clock.RealClock{},
		cleanupTimeout: 5 * time.Second,
		started:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	b.queue = newEventQueue(b.logger, loopCtx, 100)
	b.spinner = newSpinnerCoord(b.renderer, b.logger)
	b.stateMachine = newUIStateMachine(b.spinner)
	b.dispatcher = newEventDispatcher(
		b.renderer, b.logger, b.stateMachine, b.spinner,
		b.showThoughts, b.showTools, b.rawOutput, b.logFile,
	)
	b.wg.Add(1)
	return b
}
func (b *UIBridge) CloseInput() {
	b.queue.closeInput()
}
func (b *UIBridge) Cleanup() {
	b.cleanupOnce.Do(func() {
		atomic.AddInt32(&b.cleanupInvocations, 1)

		// 1. Ensure input is closed. This allows the Listen() loop to exit
		// naturally after processing all remaining events in the channel.
		b.CloseInput()

		// 2. Monitor worker completion via the WaitGroup in a separate goroutine.
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

		// 3. Wait for workers to finish gracefully, or force-kill via context after timeout.
		timer := time.NewTimer(b.cleanupTimeout)
		defer timer.Stop()

		select {
		case <-done:
			// Graceful exit: all events were drained within the timeout.
		case <-timer.C:
			// Hard stop: Drain took too long or became deadlocked.
			// Trigger the context cancellation now to unblock the Listen loop.
			b.mu.RLock()
			cancel := b.cancel
			b.mu.RUnlock()
			if cancel != nil {
				cancel()
			}
			b.logger.Warn("UI Bridge cleanup timed out")

			// Optional: Wait briefly for the loop to acknowledge the cancellation
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
		}
	})
}
func (b *UIBridge) Listen(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
	defer cancel()

	defer b.wg.Done()
	defer b.loopCancel()
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("panic in UIBridge loop", "error", r, "stack", string(debug.Stack()))
			b.spinner.stopActiveSpinner()
			err = fmt.Errorf("uibridge panicked: %v", r)
		}
	}()

	b.startOnce.Do(func() {
		close(b.started)
	})

	for {
		select {
		case <-ctx.Done():
			// Forced abort: Drain remaining events if any, but don't block forever.
			// This ensures that even if cancellation is triggered (e.g., via timeout),
			// what was already in the channel is processed if the renderer is free.
			b.queue.drainRemainingEvents(b.processRecoverable)
			b.spinner.stopActiveSpinner()
			return nil
		case e, ok := <-b.queue.recv():
			if !ok {
				b.spinner.stopActiveSpinner()
				return nil
			}
			b.processRecoverable(ctx, e)
		}
	}
}

func (b *UIBridge) processRecoverable(ctx context.Context, e events.Event) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("UIBridge actor recovered from panic", "error", r)
			b.logger.Debug("UIBridge recovery stack trace", "stack", string(debug.Stack()))
			b.spinner.stopActiveSpinner()
			panic(r) // Re-panic to stop the Listen loop
		}
	}()
	b.processEvent(ctx, e)
}
func (b *UIBridge) HandleEvent(ctx context.Context, e events.Event) error {
	if b.queue.isInputClosed() {
		b.logger.Debug("Shedding event: bridge is closed")
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			// Log as Warn/Error since it's now unexpected (last-resort safety)
			b.logger.Warn("Unexpected panic in UIBridge.HandleEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return b.queue.enqueueEvent(ctx, e)
}
func (b *UIBridge) processEvent(ctx context.Context, e events.Event) {
	b.dispatcher.dispatch(ctx, e)
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
func (b *UIBridge) getLoopContext() context.Context {
	return b.loopCtx
}

func (b *UIBridge) WaitStarted() {
	<-b.started
}
