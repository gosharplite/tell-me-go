// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

// uiSubscriber translates domain events into UI updates.
type uiSubscriber struct {
	renderer     ui.UIRenderer
	showThoughts bool
	showTools    bool
	rawOutput    bool
	useColor     bool
	logFile      string
}

// newUISubscriber creates a new uiSubscriber.
func newUISubscriber(renderer ui.UIRenderer, showThoughts, showTools, rawOutput, useColor bool, logFile string) *uiSubscriber {
	return &uiSubscriber{
		renderer:     renderer,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		useColor:     useColor,
		logFile:      logFile,
	}
}

// HandleEvent processes a domain event and updates the UI.
func (s *uiSubscriber) HandleEvent(e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		s.handleTurnStatusEvent(ev)
	case events.ResponseStreamEvent:
		s.handleTokenEvent(ev)
	case events.UsageMetricsEvent:
		s.handleFinalCostEvent(ev)
	case events.ToolCallEvent:
		s.handleToolCallEvent(ev)
	case events.ToolResultEvent:
		s.handleToolResponseEvent(ev)
	case events.SystemMessageEvent:
		s.handleSystemMessageEvent(ev)
	case events.StatusUpdate:
		s.handleStatusUpdate(ev)
	}
}

func (s *uiSubscriber) handleTurnStatusEvent(ev events.TurnStatusEvent) {
	s.renderer.LogTurnStatus(ev.Status)
}

func (s *uiSubscriber) handleTokenEvent(ev events.ResponseStreamEvent) {
	ctx := s.ensureContext(ev.Context, "ResponseStreamEvent")
	uiCh, uiFinalize := s.renderer.StreamResponse(ctx, s.showThoughts, s.rawOutput)
	s.relayStream(ctx, ev.Stream, uiCh)
	_ = uiFinalize()
}

func (s *uiSubscriber) handleFinalCostEvent(ev events.UsageMetricsEvent) {
	ctx := s.ensureContext(ev.Context, "UsageMetricsEvent")
	s.renderer.LogUsage(ctx, ev.Metrics, s.logFile, ev.StartTime)
}

func (s *uiSubscriber) handleToolCallEvent(ev events.ToolCallEvent) {
	s.renderer.LogToolCall(ev.Calls, ev.Turn, ev.MaxTurns, s.showTools)
}

func (s *uiSubscriber) handleToolResponseEvent(ev events.ToolResultEvent) {
	s.renderer.LogToolResult(ev.Name, ev.Result, s.showTools)
}

func (s *uiSubscriber) handleSystemMessageEvent(ev events.SystemMessageEvent) {
	s.renderer.LogSystemMessage(ev.Message, ev.Level)
}

func (s *uiSubscriber) handleStatusUpdate(ev events.StatusUpdate) {
	s.renderer.LogSystemMessage(ev.Message, ev.Level)
}

func (s *uiSubscriber) ensureContext(ctx context.Context, name string) context.Context {
	if ctx == nil {
		s.renderer.LogSystemMessage(name+" missing context", "warn")
		return context.Background()
	}
	return ctx
}

func (s *uiSubscriber) relayStream(ctx context.Context, stream <-chan *llm.Content, uiCh chan<- *llm.Content) {
	for {
		if !s.relayNext(ctx, stream, uiCh) {
			return
		}
	}
}

func (s *uiSubscriber) relayNext(ctx context.Context, stream <-chan *llm.Content, uiCh chan<- *llm.Content) bool {
	select {
	case <-ctx.Done():
		return false
	case c, ok := <-stream:
		if !ok {
			return false
		}
		return s.sendToUI(ctx, uiCh, c)
	}
}

func (s *uiSubscriber) sendToUI(ctx context.Context, uiCh chan<- *llm.Content, c *llm.Content) bool {
	select {
	case uiCh <- c:
		return true
	case <-ctx.Done():
		return false
	}
}
