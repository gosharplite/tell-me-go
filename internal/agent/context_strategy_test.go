// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/types"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

type mockToolRegistry struct {
	declarations []*types.ToolDeclaration
}

func (m *mockToolRegistry) GetDeclarations() []*types.ToolDeclaration {
	return m.declarations
}

func TestContextStrategy_EstimateValueSize(t *testing.T) {
	cs := &ContextStrategy{}
	tests := []struct {
		name  string
		input interface{}
		want  int
	}{
		{"string", "hello", 5},
		{"int", 123, 10},
		{"float", 123.45, 10},
		{"bool", true, 5},
		{"nil", nil, 4},
		{"slice", []interface{}{1, "a"}, 11}, // 10 + 1
		{"map", map[string]interface{}{"key": "val"}, 6}, // "key"(3) + "val"(3)
		{"nested", map[string]any{"s": []any{1, 2}}, 21}, // "s" (1) + (10+10)
		{"deeply_nested", map[string]any{
			"a": map[string]any{
				"b": []any{
					map[string]any{"c": 1},
					2,
				},
			},
		}, 23},
		{"unknown", struct{}{}, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cs.estimateValueSize(tt.input); got != tt.want {
				t.Errorf("estimateValueSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextStrategy_EstimateTokens(t *testing.T) {
	registry := &mockToolRegistry{}
	cs := NewContextStrategy(registry)

	t.Run("Base overhead", func(t *testing.T) {
		registry.declarations = nil
		// charCount = 1000. 1000 / 3.2 = 312
		got := cs.EstimateTokens(nil)
		if got != 312 {
			t.Errorf("expected 312 tokens, got %d", got)
		}
	})

	t.Run("Tool Declarations", func(t *testing.T) {
		registry.declarations = []*types.ToolDeclaration{
			{
				Name:        "my_tool",
				Description: "does something",
				Parameters:  &types.Schema{Type: "object"},
			},
		}
		// charCount = 1000 (base) + len("my_tool") (7) + len("does something") (14) + 200 (params)
		// total = 1000 + 7 + 14 + 200 = 1221
		// 1221 / 3.2 = 381.5625 -> 381
		got := cs.EstimateTokens(nil)
		if got != 381 {
			t.Errorf("expected 381 tokens, got %d", got)
		}
		registry.declarations = nil // reset
	})

	t.Run("Blob Handling", func(t *testing.T) {
		contents := []*types.Content{
			{
				Parts: []*types.Part{
					{
						InlineData: &types.Blob{
							MIMEType: "image/png",
							Data:     make([]byte, 10000), // Large blob
						},
					},
				},
			},
		}
		base := cs.EstimateTokens(nil)
		withBlob := cs.EstimateTokens(contents)
		diff := withBlob - base
		if diff != 50 {
			t.Errorf("expected blob to add 50 tokens, added %d", diff)
		}
	})

	t.Run("Recursive Map/Slice", func(t *testing.T) {
		contents := []*types.Content{
			{
				Parts: []*types.Part{
					{
						FunctionCall: &types.FunctionCall{
							Name: "test",
							Args: map[string]interface{}{
								"nested": []interface{}{1, 2},
							},
						},
					},
				},
			},
		}
		// charCount: 1000 (base) + 4 (name "test") + 6 (key "nested") + 20 (slice [1,2] -> 10+10)
		// total = 1030. 1030 / 3.2 = 321.875 -> 321
		got := cs.EstimateTokens(contents)
		if got != 321 {
			t.Errorf("expected 321 tokens, got %d", got)
		}
	})
}

func TestContextStrategy_GetWarnings_EdgeCases(t *testing.T) {
	cs := NewContextStrategy(&mockToolRegistry{})
	cs.SetLimits(1000, 10, 100)

	t.Run("Exact Boundaries - 90%", func(t *testing.T) {
		// 899/1000 = 89.9%
		w899 := cs.getTokenWarning(899)
		if w899 != "" {
			t.Errorf("expected no warning at 89.9%%, got %q", w899)
		}

		// 901/1000 = 90.1%
		w901 := cs.getTokenWarning(901)
		if w901 == "" {
			t.Error("expected warning at 90.1%")
		}
	})

	t.Run("Exact Boundaries - 95%", func(t *testing.T) {
		// 949/1000 = 94.9%
		w949 := cs.getTokenWarning(949)
		if !contains(w949, "90%") {
			t.Errorf("expected 90%% warning at 94.9%%, got %q", w949)
		}

		// 951/1000 = 95.1%
		w951 := cs.getTokenWarning(951)
		if !contains(w951, "CRITICAL") || !contains(w951, "95%") {
			t.Errorf("expected critical 95%% warning at 95.1%%, got %q", w951)
		}
	})

	t.Run("History Turn Warnings", func(t *testing.T) {
		// maxHistoryTurns = 100
		// 90.1 turns -> ratio 0.901
		w90 := cs.getHistoryTurnWarning(91)
		if !contains(w90, "90%") {
			t.Errorf("expected 90%% turn warning at 91 turns, got %q", w90)
		}

		// 95.1 turns -> ratio 0.951
		w95 := cs.getHistoryTurnWarning(96)
		if !contains(w95, "95%") {
			t.Errorf("expected 95%% turn warning at 96 turns, got %q", w95)
		}

		// 100 turns -> ratio 1.0
		w100 := cs.getHistoryTurnWarning(100)
		if !contains(w100, "limit has been reached") {
			t.Errorf("expected reach limit warning at 100 turns, got %q", w100)
		}
	})

	t.Run("Turn Count Limits", func(t *testing.T) {
		// maxToolTurns = 10
		// turn 7 -> 3 remaining
		w3 := cs.getTurnWarning(7)
		if !contains(w3, "3 turns remaining") {
			t.Errorf("expected warning at 3 turns remaining, got %q", w3)
		}
		// turn 8 -> 2 remaining
		w2 := cs.getTurnWarning(8)
		if !contains(w2, "URGENT") || !contains(w2, "2 turns remaining") {
			t.Errorf("expected urgent warning at 2 turns remaining, got %q", w2)
		}
		// turn 9 -> 1 remaining
		w1 := cs.getTurnWarning(9)
		if !contains(w1, "FINAL") || !contains(w1, "final turn") {
			t.Errorf("expected final warning at 1 turn remaining, got %q", w1)
		}
	})

	t.Run("Pruning Counter Reset", func(t *testing.T) {
		cs.SetPrunedTurns(10)
		_ = cs.GetWarnings(1, 10, 1) // First call triggers and resets
		if cs.prunedTurns != 0 {
			t.Errorf("expected prunedTurns to be reset, got %d", cs.prunedTurns)
		}
	})
}
