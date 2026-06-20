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
	// StartSpinner begins an indeterminate progress indicator and returns
	// a stop function. Callers must invoke stop() exactly once to clear
	// the spinner. The spinner runs until stop is called or the context
	// is cancelled.
	StartSpinner(ctx context.Context) (stop func())

	// StartSpinnerWithStatus begins a spinner with the given status text
	// displayed alongside the indicator.
	StartSpinnerWithStatus(ctx context.Context, status string) (stop func())

	// StartSpinnerWithMetrics begins a spinner with status text and
	// live-updating resource usage metrics (CPU, memory).
	StartSpinnerWithMetrics(ctx context.Context, status string) (stop func())

	// RenderResponse writes the LLM response to the terminal. When
	// showThoughts is true, reasoning/thinking blocks are included.
	// When rawOutput is true, markdown rendering is disabled.
	RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool)
}

// StatusLogger defines the interface for logging status and system messages.
type StatusLogger interface {
	// LogTurnStatus renders the outcome of a conversation turn (e.g.,
	// "completed", "error", "cancelled").
	LogTurnStatus(ctx context.Context, status events.TurnStatus)

	// LogSystemMessage renders a system-level message with the given
	// severity level (e.g., "info", "warn", "error").
	LogSystemMessage(ctx context.Context, msg string, level string)

	// RenderHealthReport displays the consolidated health report to
	// the user. The report includes per-component status and timestamps.
	RenderHealthReport(ctx context.Context, report *HealthReport)
}

// UsageLogger defines the interface for logging usage metrics.
type UsageLogger interface {
	// LogUsage writes token usage, cost, and latency metrics to the
	// terminal and optionally to the specified log file. startTime is
	// the time the LLM request was initiated, used to compute duration.
	LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time)
}

// ToolLogger defines the interface for logging tool calls and results.
type ToolLogger interface {
	// LogToolCall renders a tool invocation to the user. turn and
	// maxTurns provide progress context (e.g., "tool 2/5").
	// When showTools is false, the call is logged at debug level only.
	LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool)

	// LogToolResult renders the outcome of a tool execution. When
	// showTools is false, only errors are displayed.
	LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool)
}

// RendererConfigurator defines the interface for configuring renderer behavior.
type RendererConfigurator interface {
	// SetUseColor enables or disables ANSI color output for subsequent
	// rendering operations. Safe to call at any time.
	SetUseColor(use bool)

	// SetForceSpinner forces the spinner to appear even when output is
	// not a terminal. Useful for testing or when piping to a pager.
	SetForceSpinner(force bool)

	// IsTerminalContext reports whether the renderer is connected to
	// an interactive terminal. This determines default spinner behavior.
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
	// Render writes a formatted representation of the last n entries
	// from the HistoryReader to w. The options control formatting:
	// Raw disables markdown rendering, ShowThoughts includes reasoning
	// blocks, and UseColor enables ANSI color output.
	Render(w io.Writer, h HistoryReader, n int, options HistoryRenderOptions)
}

// HistoryRenderOptions defines the options for rendering chat history.
type HistoryRenderOptions struct {
	// Raw disables markdown rendering and special formatting.
	Raw bool
	// ShowThoughts includes extracted reasoning/thinking blocks in the output.
	ShowThoughts bool
	// UseColor enables ANSI color escape sequences in the output.
	UseColor bool
	// CustomRenderer, if non-nil, is used instead of creating a glamour
	// TermRenderer. The value must have a Render(string)(string,error) method.
	// Intended for testing. Ignored when Raw is true.
	CustomRenderer interface{ Render(string) (string, error) }
}

// SystemMetricsProvider defines the interface for collecting host resource usage.
type SystemMetricsProvider interface {
	// GetCPUStats returns (total, idle) ticks or seconds.
	GetCPUStats() (total int64, idle int64)
	// GetMemoryPercent returns the host memory usage percentage (0-100).
	GetMemoryPercent() float64
}
