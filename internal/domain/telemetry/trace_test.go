// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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

func TestTurnTrace_CumulativeToolDuration(t *testing.T) {
	tests := []struct {
		name       string
		traces     []ToolExecutionTrace
		expected   time.Duration
		isNilTrace bool
	}{
		{
			name:       "nil trace",
			isNilTrace: true,
			expected:   0,
		},
		{
			name:     "no executions",
			traces:   []ToolExecutionTrace{},
			expected: 0,
		},
		{
			name: "single execution",
			traces: []ToolExecutionTrace{
				{Duration: time.Second},
			},
			expected: time.Second,
		},
		{
			name: "multiple executions",
			traces: []ToolExecutionTrace{
				{Duration: time.Second},
				{Duration: 500 * time.Millisecond},
				{Duration: 2 * time.Second},
			},
			expected: 3500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var trace *TurnTrace
			if !tt.isNilTrace {
				trace = NewTurnTrace()
				for _, te := range tt.traces {
					trace.RecordToolExecution(te)
				}
			}

			actual := trace.CumulativeToolDuration()
			if actual != tt.expected {
				t.Errorf("CumulativeToolDuration() = %v, want %v", actual, tt.expected)
			}
		})
	}
}

// TestTurnTrace_WarningsJSON pins the JSON contract of the general Warnings
// field (ADR-068 §8): present with the "warnings" key when populated, and
// omitted (omitempty) when empty.
func TestTurnTrace_WarningsJSON(t *testing.T) {
	t.Run("warnings present marshal with key", func(t *testing.T) {
		tt := NewTurnTrace()
		tt.Warnings = []string{"injected_engrams:e1,e2"}

		data, err := json.Marshal(tt)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !strings.Contains(string(data), `"warnings":["injected_engrams:e1,e2"]`) {
			t.Errorf("marshaled JSON = %s; want warnings key present", data)
		}
	})

	t.Run("empty warnings omit key", func(t *testing.T) {
		tt := NewTurnTrace() // Warnings is nil

		data, err := json.Marshal(tt)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(data), "warnings") {
			t.Errorf("marshaled JSON = %s; want warnings key omitted (omitempty)", data)
		}
	})
}

// TestTurnTrace_AddWarnings pins the nil-receiver fail-open guard, the
// empty-warnings no-op, and the append-under-lock path.
func TestTurnTrace_AddWarnings(t *testing.T) {
	t.Run("nil receiver fails open", func(t *testing.T) {
		var tr *TurnTrace
		tr.AddWarnings("w") // must not panic; no-op
	})
	t.Run("empty warnings no-op", func(t *testing.T) {
		tr := NewTurnTrace()
		tr.AddWarnings()
		if len(tr.Warnings) != 0 {
			t.Errorf("AddWarnings() with no args appended %d entries, want 0", len(tr.Warnings))
		}
	})
	t.Run("appends warnings", func(t *testing.T) {
		tr := NewTurnTrace()
		tr.AddWarnings("a", "b")
		if !reflect.DeepEqual(tr.Warnings, []string{"a", "b"}) {
			t.Errorf("AddWarnings() = %v, want [a b]", tr.Warnings)
		}
	})
}
