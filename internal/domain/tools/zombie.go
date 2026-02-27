// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"errors"
	"time"
)

// ExecutionObserver defines the domain's external monitoring needs.
type ExecutionObserver interface {
	ExecutionTimedOut(toolID string)
	ExecutionCompletedLate(toolID string)
}

// ToolOutput captures the result and error of a tool execution.
// It is used to monitor goroutines that may outlive their context.
type ToolOutput struct {
	Result ToolResult
	Err    error
}

// ZombieTool handles the monitoring of potentially leaked tool goroutines.
type ZombieTool struct {
	observer ExecutionObserver
}

// NewZombieTool creates a new ZombieTool with the given observer.
func NewZombieTool(observer ExecutionObserver) (*ZombieTool, error) {
	if observer == nil {
		return nil, errors.New("ExecutionObserver is required")
	}
	return &ZombieTool{observer: observer}, nil
}

// Monitor blocks until the tool finishes, the context is cancelled, or the zombie timeout is reached.
func (z *ZombieTool) Monitor(ctx context.Context, name string, start time.Time, outCh <-chan ToolOutput, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-outCh:
		// Tool eventually finished, log the extreme latency
		z.observer.ExecutionCompletedLate(name)
	case <-timer.C:
		z.observer.ExecutionTimedOut(name)
	case <-ctx.Done():
		return
	}
}
