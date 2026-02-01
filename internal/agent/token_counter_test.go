// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"
	"time"
)

func TestHeuristicTokenCounter_EstimateMapSize(t *testing.T) {
	htc := &HeuristicTokenCounter{}

	tests := []struct {
		name     string
		input    map[string]interface{}
		expected int
	}{
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: 0,
		},
		{
			name: "flat map",
			input: map[string]interface{}{
				"key1": "value1", // len("key1")=4 + len("value1")=6 = 10
				"key2": 123,      // len("key2")=4 + 10 (int) = 14
			},
			expected: 24,
		},
		{
			name: "nested map",
			input: map[string]interface{}{
				"outer": map[string]interface{}{ // len("outer")=5
					"inner": "val", // len("inner")=5 + len("val")=3 = 8
				},
			},
			expected: 5 + 8,
		},
		{
			name: "map with slice",
			input: map[string]interface{}{
				"list": []interface{}{"a", "b"}, // len("list")=4 + (len("a")=1 + len("b")=1 + 1 (overhead)) = 4 + 3 = 7
			},
			expected: 7,
		},
		{
			name:     "nil map",
			input:    nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htc.EstimateMapSize(tt.input)
			if got != tt.expected {
				t.Errorf("EstimateMapSize() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultRetryPolicy_Coverage(t *testing.T) {
	policy := &DefaultRetryPolicy{MaxRetries: 2, Backoff: 10 * time.Millisecond}

	t.Run("Transient error", func(t *testing.T) {
		err := &AgentError{Category: ErrTransient, Message: "retry"}
		delay, retry := policy.ShouldRetry(err, 0)
		if !retry || delay != 10*time.Millisecond {
			t.Errorf("expected retry with 10ms, got %v, %v", retry, delay)
		}

		delay, retry = policy.ShouldRetry(err, 1)
		if !retry || delay != 20*time.Millisecond {
			t.Errorf("expected retry with 20ms, got %v, %v", retry, delay)
		}

		_, retry = policy.ShouldRetry(err, 2)
		if retry {
			t.Error("expected no retry after MaxRetries")
		}
	})

	t.Run("Fatal error", func(t *testing.T) {
		err := &AgentError{Category: ErrFatal, Message: "fatal"}
		_, retry := policy.ShouldRetry(err, 0)
		if retry {
			t.Error("expected no retry for fatal error")
		}
	})

	t.Run("Generic error", func(t *testing.T) {
		// If err is nil, it returns false.
		_, retry := policy.ShouldRetry(nil, 0)
		if retry {
			t.Error("expected no retry for nil error")
		}
	})
}
