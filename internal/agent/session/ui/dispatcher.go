// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"reflect"
	"time"

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
	d.register(events.ResponseEvent{}, d.handleResponse)
	d.register(events.UsageMetricsEvent{}, d.handleUsageMetrics)
	d.register(events.ToolCallEvent{}, d.handleToolEvents)
	d.register(events.ToolResultEvent{}, d.handleToolEvents)
	d.register(events.TurnStarted{}, d.handleTurnStarted)
	d.register(events.SystemMessageEvent{}, d.handleSystemMessage)
	d.register(events.StatusUpdate{}, d.handleSystemMessage)
	d.register(events.ToolOutputStreamEvent{}, d.handleToolOutputStream)
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
	// When tracking turn time, don't transition to idle — it would
	// stop the spinner and break the elapsed-time counter.
	if !d.spinner.turnStartTime.IsZero() {
		d.renderer.LogTurnStatus(ctx, ev.Status)
		return
	}
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

func (d *eventDispatcher) handleResponse(ctx context.Context, e events.Event) {
	ev := e.(events.ResponseEvent)
	d.spinner.activePhase = nil // Clear phase on response
	// When tracking turn time, don't transition to rendering — it would
	// stop the spinner and break the elapsed-time counter.
	if !d.spinner.turnStartTime.IsZero() {
		d.renderer.RenderResponse(ctx, ev.Content, d.showThoughts, d.rawOutput)
		return
	}
	d.stateMachine.transition(stateRendering)
	d.renderer.RenderResponse(ctx, ev.Content, d.showThoughts, d.rawOutput)
}

func (d *eventDispatcher) handleUsageMetrics(_ context.Context, e events.Event) {
	ev := e.(events.UsageMetricsEvent)
	ctx := d.ensureContext(ev.Context, "UsageMetricsEvent")
	// When turn-time tracking is active, log without stopping the spinner.
	if !d.spinner.turnStartTime.IsZero() {
		d.renderer.LogUsage(ctx, ev.Metrics, d.logFile, ev.StartTime)
		return
	}
	d.spinner.stopActiveSpinner()
	d.renderer.LogUsage(ctx, ev.Metrics, d.logFile, ev.StartTime)
	if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
		d.stateMachine.setState(stateThinking)
	}
}

func (d *eventDispatcher) handleToolEvents(ctx context.Context, e events.Event) {
	ctx = d.ensureContext(ctx, "handleToolEvents")

	switch ev := e.(type) {
	case events.ToolCallEvent:
		if ev.Calls == nil {
			d.logger.Debug("handleToolEvents: ToolCallEvent missing Calls")
			return
		}
		// When turn-time tracking is active, ToolExecutionStartedEvent follows
		// immediately and handles the spinner transition in-place. Skip stop/resume
		// to preserve the elapsed-time counter.
		if !d.spinner.turnStartTime.IsZero() {
			d.renderer.LogToolCall(ctx, ev.Calls, ev.Turn, ev.MaxTurns, d.showTools)
			return
		}
		d.spinner.stopActiveSpinner()
		d.renderer.LogToolCall(ctx, ev.Calls, ev.Turn, ev.MaxTurns, d.showTools)
		if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
			d.stateMachine.setState(stateThinking)
		}
	case events.ToolResultEvent:
		if ev.Name == "" {
			d.logger.Debug("handleToolEvents: ToolResultEvent missing Name")
			return
		}
		// When turn-time tracking is active, avoid stopping the spinner so the
		// elapsed-time counter continues. The next phase transition will update
		// the status in-place.
		if !d.spinner.turnStartTime.IsZero() {
			d.renderer.LogToolResult(ctx, ev.Name, ev.Result, d.showTools)
			return
		}
		d.spinner.stopActiveSpinner()
		d.renderer.LogToolResult(ctx, ev.Name, ev.Result, d.showTools)
		if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
			d.stateMachine.setState(stateThinking)
		}
	default:
		d.logger.Debug("handleToolEvents: unexpected event type", "type", reflect.TypeOf(e))
	}
}

func (d *eventDispatcher) handleTurnStarted(_ context.Context, _ events.Event) {
	d.spinner.activePhase = nil
	d.stateMachine.transition(stateIdle)
	d.spinner.SetTurnStartTime(time.Now())
}

func (d *eventDispatcher) handleSystemMessage(ctx context.Context, e events.Event) {
	var msg, lvl string

	switch ev := e.(type) {
	case events.SystemMessageEvent:
		msg, lvl = ev.Message, ev.Level
	case events.StatusUpdate:
		msg, lvl = ev.Message, ev.Level
	case events.ToolOutputStreamEvent:
		msg, lvl = ev.Message, ev.Level
	default:
		return
	}
	// When turn-time tracking is active, log without stopping the spinner.
	if !d.spinner.turnStartTime.IsZero() {
		d.renderer.LogSystemMessage(ctx, msg, lvl)
		return
	}
	d.spinner.stopActiveSpinner()
	d.renderer.LogSystemMessage(ctx, msg, lvl)
	if d.spinner.resumeActiveSpinner(ctx, d.stateMachine.current(), nil) {
		d.stateMachine.setState(stateThinking)
	}
}

func (d *eventDispatcher) handleToolOutputStream(ctx context.Context, e events.Event) {
	ev := e.(events.ToolOutputStreamEvent)
	d.renderer.LogSystemMessage(ctx, ev.Message, ev.Level)
}

// --- helpers ------------------------------------------------------------------

func (d *eventDispatcher) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		d.logger.Debug(name + " missing context")
		return context.Background()
	}
	return ctx
}
