// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewTurnTrace(t *testing.T) {
	tt := NewTurnTrace()
	if tt == nil {
		t.Fatal("NewTurnTrace() returned nil")
	}
	if tt.StartTime.IsZero() {
		t.Error("expected StartTime to be non-zero")
	}
	if tt.ToolExecutions == nil {
		t.Error("expected ToolExecutions to be initialized")
	}
	if len(tt.ToolExecutions) != 0 {
		t.Errorf("expected 0 executions, got %d", len(tt.ToolExecutions))
	}
}

func TestContextPropagation(t *testing.T) {
	t.Run("ValidTrace", func(t *testing.T) {
		expected := NewTurnTrace()
		ctx := ContextWithTrace(context.Background(), expected)
		actual := TraceFromContext(ctx)
		if actual != expected {
			t.Errorf("expected trace %v, got %v", expected, actual)
		}
	})

	t.Run("EmptyContext", func(t *testing.T) {
		actual := TraceFromContext(context.Background())
		if actual != nil {
			t.Errorf("expected nil trace, got %v", actual)
		}
	})

	t.Run("WrongValueType", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), traceKey, "not-a-trace")
		actual := TraceFromContext(ctx)
		if actual != nil {
			t.Errorf("expected nil trace for wrong type, got %v", actual)
		}
	})
}

func TestTurnTrace_RecordToolExecution(t *testing.T) {
	t.Run("NilReceiver", func(t *testing.T) {
		var tt *TurnTrace
		// Should not panic
		tt.RecordToolExecution(ToolExecutionTrace{ToolName: "test"})
	})

	t.Run("SingleExecution", func(t *testing.T) {
		tt := NewTurnTrace()
		trace := ToolExecutionTrace{
			ToolName:  "test-tool",
			StartTime: time.Now(),
			Duration:  time.Second,
			Status:    "success",
		}
		tt.RecordToolExecution(trace)

		if len(tt.ToolExecutions) != 1 {
			t.Errorf("expected 1 execution, got %d", len(tt.ToolExecutions))
		}
		if tt.ToolExecutions[0].ToolName != "test-tool" {
			t.Errorf("expected tool name 'test-tool', got '%s'", tt.ToolExecutions[0].ToolName)
		}
	})

	t.Run("Concurrency", func(t *testing.T) {
		tt := NewTurnTrace()
		const count = 100
		var wg sync.WaitGroup
		wg.Add(count)
		for i := 0; i < count; i++ {
			go func(n int) {
				defer wg.Done()
				tt.RecordToolExecution(ToolExecutionTrace{
					ToolName: fmt.Sprintf("tool-%d", n),
				})
			}(i)
		}
		wg.Wait()
		if len(tt.ToolExecutions) != count {
			t.Errorf("expected %d executions, got %d", count, len(tt.ToolExecutions))
		}
	})
}
