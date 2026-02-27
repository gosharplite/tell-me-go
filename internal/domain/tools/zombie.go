// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"time"
)

// Logger defines a simple logging interface for tool execution monitoring.
// It decouples the monitoring logic from specific logging implementations.
type Logger interface {
	LogCritical(msg string)
	RecordLateCompletion(name string, d time.Duration)
}

// ToolOutput captures the result and error of a tool execution.
// It is used to monitor goroutines that may outlive their context.
type ToolOutput struct {
	Result ToolResult
	Err    error
}

// ZombieTool handles the monitoring of potentially leaked tool goroutines.
type ZombieTool struct {
	logger Logger
}

// NewZombieTool creates a new ZombieTool with the given logger.
func NewZombieTool(logger Logger) *ZombieTool {
	return &ZombieTool{logger: logger}
}

// Monitor blocks until the tool finishes, the context is cancelled, or the zombie timeout is reached.
func (z *ZombieTool) Monitor(ctx context.Context, name string, start time.Time, outCh <-chan ToolOutput, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-outCh:
		// Tool eventually finished, log the extreme latency
		z.logger.RecordLateCompletion(name, time.Since(start))
	case <-timer.C:
		z.logger.LogCritical("CRITICAL: Tool goroutine permanently leaked: " + name)
	case <-ctx.Done():
		return
	}
}
