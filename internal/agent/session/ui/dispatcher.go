// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"reflect"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// eventHandler is the function signature for all event type handlers.
type eventHandler func(ctx context.Context, e events.Event)

// eventDispatcher routes domain events to registered handlers using a
// reflect.Type-based dispatch map. New event types are added by writing
// a handler method and calling register() in the constructor — no changes
// to the dispatch logic itself.
type eventDispatcher struct {
	handlers     map[reflect.Type]eventHandler
	renderer     ports.UIRenderer
	logger       ports.Logger
	stateMachine *uiStateMachine
	spinner      *spinnerCoord
	showThoughts bool
	showTools    bool
	rawOutput    bool
	logFile      string
}

// newEventDispatcher creates a new eventDispatcher and registers all known
// event type handlers.
func newEventDispatcher(
	renderer ports.UIRenderer,
	logger ports.Logger,
	sm *uiStateMachine,
	sc *spinnerCoord,
	showThoughts, showTools, rawOutput bool,
	logFile string,
) *eventDispatcher {
	d := &eventDispatcher{
		handlers:     make(map[reflect.Type]eventHandler),
		renderer:     renderer,
		logger:       logger,
		stateMachine: sm,
		spinner:      sc,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		logFile:      logFile,
	}
	d.register(events.TurnStatusEvent{}, d.handleTurnStatus)
	d.register(events.InferenceStartedEvent{}, d.handleSpinnerEvent)
	d.register(events.SummarizationStartedEvent{}, d.handleSpinnerEvent)
	d.register(events.ToolExecutionStartedEvent{}, d.handleSpinnerEvent)
	d.register(events.RetryWaitingEvent{}, d.handleSpinnerEvent)
	d.register(events.ConsentStartedEvent{}, d.handleConsentStarted)
	d.register(events.ConsentFinishedEvent{}, d.handleConsentFinished)
	d.register(events.ResponseEvent{}, d.handleResponse)
	d.register(events.UsageMetricsEvent{}, d.handleUsageMetrics)
	d.register(events.ToolCallEvent{}, d.handleToolEvents)
	d.register(events.ToolResultEvent{}, d.handleToolEvents)
	d.register(events.TurnStarted{}, d.handleTurnStarted)
	d.register(events.SystemMessageEvent{}, d.handleSystemMessage)
	d.register(events.StatusUpdate{}, d.handleSystemMessage)
	return d
}

func (d *eventDispatcher) register(e events.Event, h eventHandler) {
	d.handlers[reflect.TypeOf(e)] = h
}

// dispatch looks up the handler for the event's concrete type and invokes it.
// Unknown event types are silently ignored (no-op).
func (d *eventDispatcher) dispatch(ctx context.Context, e events.Event) {
	if h, ok := d.handlers[reflect.TypeOf(e)]; ok {
		h(ctx, e)
	}
}

// --- handler methods ----------------------------------------------------------

func (d *eventDispatcher) handleTurnStatus(ctx context.Context, e events.Event) {
	ev := e.(events.TurnStatusEvent)
	d.spinner.activePhase = nil // Clear phase on new turn/header
	d.stateMachine.transition(stateIdle)
	d.renderer.LogTurnStatus(ctx, ev.Status)
}

func (d *eventDispatcher) handleSpinnerEvent(ctx context.Context, e events.Event) {
	d.spinner.activePhase = e
	started := d.spinner.startSpinnerForPhase(ctx, e, d.stateMachine.current(), func() uiState {
		d.stateMachine.transition(stateIdle)
		return d.stateMachine.current()
	})
	if started {
		d.stateMachine.setState(stateThinking)
	}
}

func (d *eventDispatcher) handleConsentStarted(_ context.Context, _ events.Event) {
	d.stateMachine.transition(stateAwaitingConsent)
}

func (d *eventDispatcher) handleConsentFinished(ctx context.Context, _ events.Event) {
	d.stateMachine.transition(stateIdle)
	if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
		d.stateMachine.setState(stateThinking)
	}
}

func (d *eventDispatcher) handleResponse(ctx context.Context, e events.Event) {
	ev := e.(events.ResponseEvent)
	d.spinner.activePhase = nil // Clear phase on response
	d.stateMachine.transition(stateRendering)
	d.renderer.RenderResponse(ctx, ev.Content, d.showThoughts, d.rawOutput)
}

func (d *eventDispatcher) handleUsageMetrics(_ context.Context, e events.Event) {
	ev := e.(events.UsageMetricsEvent)
	ctx := d.ensureContext(ev.Context, "UsageMetricsEvent")
	d.spinner.stopActiveSpinner()
	d.renderer.LogUsage(ctx, ev.Metrics, d.logFile, ev.StartTime)
	if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
		d.stateMachine.setState(stateThinking)
	}
}

func (d *eventDispatcher) handleToolEvents(ctx context.Context, e events.Event) {
	switch ev := e.(type) {
	case events.ToolCallEvent:
		d.spinner.stopActiveSpinner()
		d.renderer.LogToolCall(ctx, ev.Calls, ev.Turn, ev.MaxTurns, d.showTools)
		if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
			d.stateMachine.setState(stateThinking)
		}
	case events.ToolResultEvent:
		d.spinner.stopActiveSpinner()
		d.renderer.LogToolResult(ctx, ev.Name, ev.Result, d.showTools)
		if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
			d.stateMachine.setState(stateThinking)
		}
	}
}

func (d *eventDispatcher) handleTurnStarted(_ context.Context, _ events.Event) {
	d.spinner.activePhase = nil
	d.stateMachine.transition(stateIdle)
}

func (d *eventDispatcher) handleSystemMessage(ctx context.Context, e events.Event) {
	var msg, lvl string

	switch ev := e.(type) {
	case events.SystemMessageEvent:
		msg, lvl = ev.Message, ev.Level
	case events.StatusUpdate:
		msg, lvl = ev.Message, ev.Level
	default:
		return
	}
	d.spinner.stopActiveSpinner()
	d.renderer.LogSystemMessage(ctx, msg, lvl)
	if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
		d.stateMachine.setState(stateThinking)
	}
}

// --- helpers ------------------------------------------------------------------

func (d *eventDispatcher) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		d.logger.Debug(name + " missing context")
		return context.Background()
	}
	return ctx
}
