// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ResponseRenderer defines the interface for synchronous response rendering.
type ResponseRenderer interface {
	StartSpinner(ctx context.Context) (stop func())
	StartSpinnerWithStatus(ctx context.Context, status string) (stop func())
	StartSpinnerWithMetrics(ctx context.Context, status string) (stop func())
	RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool)
}

// StatusLogger defines the interface for logging status and system messages.
type StatusLogger interface {
	LogTurnStatus(ctx context.Context, status events.TurnStatus)
	LogSystemMessage(ctx context.Context, msg string, level string)
	RenderHealthReport(ctx context.Context, report *HealthReport)
}

// UsageLogger defines the interface for logging usage metrics.
type UsageLogger interface {
	LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time)
}

// ToolLogger defines the interface for logging tool calls and results.
type ToolLogger interface {
	LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool)
	LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool)
}

// RendererConfigurator defines the interface for configuring renderer behavior.
type RendererConfigurator interface {
	SetUseColor(use bool)
	SetForceSpinner(force bool)
	IsTerminalContext() bool
}

// UIRenderer defines the interface for UI feedback.
type UIRenderer interface {
	ResponseRenderer
	StatusLogger
	UsageLogger
	ToolLogger
	RendererConfigurator
}

// HistoryRenderer defines the interface for rendering chat history.
type HistoryRenderer interface {
	Render(w io.Writer, h HistoryReader, n int, options HistoryRenderOptions)
}

// HistoryRenderOptions defines the options for rendering chat history.
type HistoryRenderOptions struct {
	Raw          bool
	ShowThoughts bool
	UseColor     bool
}

// SystemMetricsProvider defines the interface for collecting host resource usage.
type SystemMetricsProvider interface {
	// GetCPUStats returns (total, idle) ticks or seconds.
	GetCPUStats() (total int64, idle int64)
	// GetMemoryPercent returns the host memory usage percentage (0-100).
	GetMemoryPercent() float64
}
