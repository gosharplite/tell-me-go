// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// UIRenderer defines the interface for UI feedback.
type UIRenderer interface {
	StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content)
	LogTurnStatus(status events.TurnStatus)
	LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time)
	LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool)
	LogToolResult(name string, result tools.ToolResult, showTools bool)
	LogSystemMessage(msg string, level string)
	SetUseColor(use bool)
}

// HistoryRenderer defines the interface for rendering chat history.
type HistoryRenderer interface {
	Render(w io.Writer, h HistoryManager, n int, options HistoryRenderOptions)
}

// HistoryRenderOptions defines the options for rendering chat history.
type HistoryRenderOptions struct {
	Raw          bool
	ShowThoughts bool
	UseColor     bool
}
