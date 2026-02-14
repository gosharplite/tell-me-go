// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"sync"
	"time"
)

// ToolExecutionTrace records the lifecycle of a single tool execution.
type ToolExecutionTrace struct {
	ToolName  string        `json:"tool_name"`
	StartTime time.Time     `json:"start_time"`
	Duration  time.Duration `json:"duration"`
	Status    string        `json:"status"` // "success", "error", "circuit_open"
	Error     string        `json:"error,omitempty"`
}

// TurnTrace records the lifecycle of a single agent turn.
type TurnTrace struct {
	mu                sync.Mutex           `json:"-"`
	StartTime         time.Time            `json:"start_time"`
	EndTime           time.Time            `json:"end_time"`
	InferenceDuration time.Duration        `json:"inference_duration"`
	ToolExecutions    []ToolExecutionTrace `json:"tool_executions"`
	FinalStatus       string               `json:"final_status"`
}

type contextKey struct{}

var traceKey = contextKey{}

// NewTurnTrace creates a new TurnTrace instance.
func NewTurnTrace() *TurnTrace {
	return &TurnTrace{
		StartTime:      time.Now(),
		ToolExecutions: make([]ToolExecutionTrace, 0),
	}
}

// ContextWithTrace attaches a TurnTrace to the context.
func ContextWithTrace(ctx context.Context, t *TurnTrace) context.Context {
	return context.WithValue(ctx, traceKey, t)
}

// TraceFromContext retrieves a TurnTrace from the context.
func TraceFromContext(ctx context.Context) *TurnTrace {
	if t, ok := ctx.Value(traceKey).(*TurnTrace); ok {
		return t
	}
	return nil
}

// RecordToolExecution adds a tool execution trace to the TurnTrace.
func (t *TurnTrace) RecordToolExecution(trace ToolExecutionTrace) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ToolExecutions = append(t.ToolExecutions, trace)
}

// CumulativeToolDuration returns the sum of all tool execution durations.
func (t *TurnTrace) CumulativeToolDuration() time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var total time.Duration
	for _, te := range t.ToolExecutions {
		total += te.Duration
	}
	return total
}
