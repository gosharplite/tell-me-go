// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/ui"
)

// UISubscriber translates domain events into UI updates.
type UISubscriber struct {
	renderer     ui.UIRenderer
	showThoughts bool
	showTools    bool
	rawOutput    bool
	logFile      string
}

func NewUISubscriber(renderer ui.UIRenderer, showThoughts, showTools, rawOutput bool, logFile string) *UISubscriber {
	return &UISubscriber{
		renderer:     renderer,
		showThoughts: showThoughts,
		showTools:    showTools,
		rawOutput:    rawOutput,
		logFile:      logFile,
	}
}

func (s *UISubscriber) HandleEvent(e events.Event) {
	switch ev := e.(type) {
	case events.TurnStatusEvent:
		s.renderer.LogTurnStatus(ev.Status)
	case events.ResponseStreamEvent:
		uiCh, uiFinalize := s.renderer.StreamResponse(ev.Context, s.showThoughts, s.rawOutput)
		for c := range ev.Stream {
			uiCh <- c
		}
		_ = uiFinalize()
	case events.UsageMetricsEvent:
		s.renderer.LogUsage(ev.Metrics, s.logFile, ev.StartTime)
	case events.ToolCallEvent:
		s.renderer.LogToolCall(ev.Calls, ev.Turn, ev.MaxTurns, s.showTools)
	case events.ToolResultEvent:
		s.renderer.LogToolResult(ev.Name, ev.Result, s.showTools)
	case events.SystemMessageEvent:
		s.renderer.LogSystemMessage(ev.Message, ev.Level)
	case events.StatusUpdate:
		s.renderer.LogSystemMessage(ev.Message, ev.Level)
	}
}
