// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/agent/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
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
		ctx := ev.Context
		if ctx == nil {
			ctx = context.Background()
		}
		uiCh, uiFinalize := s.renderer.StreamResponse(ctx, s.showThoughts, s.rawOutput)
	streamLoop:
		for {
			select {
			case <-ctx.Done():
				break streamLoop
			case c, ok := <-ev.Stream:
				if !ok {
					break streamLoop
				}
				uiCh <- c
			}
		}
		_ = uiFinalize()
	case events.UsageMetricsEvent:
		ctx := ev.Context
		if ctx == nil {
			ctx = context.Background()
		}
		s.renderer.LogUsage(ctx, ev.Metrics, s.logFile, ev.StartTime)
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
