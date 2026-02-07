// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestHeuristicTokenCounter_EstimateValueSize(t *testing.T) {
	htc := &HeuristicTokenCounter{}
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
		{"slice", []interface{}{1, "a"}, 12},             // 1 + 10 + 1
		{"map", map[string]interface{}{"key": "val"}, 6}, // "key"(3) + "val"(3)
		{"nested", map[string]any{"s": []any{1, 2}}, 22}, // "s" (1) + (1+10+10)
		{"deeply_nested", map[string]any{
			"a": map[string]any{
				"b": []any{
					map[string]any{"c": 1},
					2,
				},
			},
		}, 24},
		{"unknown", struct{}{}, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := htc.EstimateValueSize(tt.input); got != tt.want {
				t.Errorf("EstimateValueSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextStrategy_EstimateTokens(t *testing.T) {
	registry := &mockToolRegistry{}
	cs := NewContextStrategy(NewHeuristicTokenCounter(registry), nil)

	t.Run("Base overhead", func(t *testing.T) {
		registry.declarations = nil
		// base = 300
		got := cs.EstimateTokens(nil)
		if got != 300 {
			t.Errorf("expected 300 tokens, got %d", got)
		}
	})

	t.Run("Tool Declarations", func(t *testing.T) {
		registry.declarations = []*tools.ToolDeclaration{
			{
				Name:        "my_tool",
				Description: "does something",
				Parameters:  &tools.Schema{Type: "object"},
			},
		}
		// charCount = base(300) + (len("my_tool") (7) + len("does something") (14)) / 4 + 50 (params)
		// total = 300 + 5 + 50 = 355
		got := cs.EstimateTokens(nil)
		if got != 355 {
			t.Errorf("expected 355 tokens, got %d", got)
		}
		registry.declarations = nil // reset
	})

	t.Run("Blob Handling", func(t *testing.T) {
		contents := []*llm.Content{
			{
				Parts: []*llm.Part{
					{
						InlineData: &llm.Blob{
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
		// 160 chars / 3.2 = 50 tokens
		if diff != 50 {
			t.Errorf("expected blob to add 50 tokens, added %d", diff)
		}
	})

	t.Run("Recursive Map/Slice", func(t *testing.T) {
		contents := []*llm.Content{
			{
				Parts: []*llm.Part{
					{
						FunctionCall: &llm.FunctionCall{
							Name: "test",
							Args: map[string]interface{}{
								"nested": []interface{}{1, 2},
							},
						},
					},
				},
			},
		}
		// charCount: (base 300) + (name "test"(4) + key "nested"(6) + slice [1,2] -> 10+10)/3.2
		// 30 / 3.2 = 9.375 -> 9
		// total = 300 + 9 = 309
		got := cs.EstimateTokens(contents)
		if got != 309 {
			t.Errorf("expected 309 tokens, got %d", got)
		}
	})
}

func setupWarningTest() *ContextStrategy {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)
	cs.SetLimits(1000, 10, 100)
	return cs
}

func TestContextStrategy_Warnings_EmptyHistory(t *testing.T) {
	cs := setupWarningTest()
	warnings := cs.GetWarnings(0, 0, 0)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty history, got %d", len(warnings))
	}
}

func TestContextStrategy_Warnings_TokenPressure(t *testing.T) {
	cs := setupWarningTest()

	t.Run("Token Boundaries", func(t *testing.T) {
		tests := []struct {
			tokens   int
			expected string
		}{
			{899, ""},
			{901, "90%"},
			{901, "manage_history"},
			{949, "90%"},
			{951, "CRITICAL"},
			{951, "95%"},
		}
		for _, tt := range tests {
			got := cs.getTokenWarning(tt.tokens)
			if tt.expected == "" {
				if got != "" {
					t.Errorf("expected no warning at %d, got %q", tt.tokens, got)
				}
			} else if !contains(got, tt.expected) {
				t.Errorf("expected warning at %d to contain %q, got %q", tt.tokens, tt.expected, got)
			}
		}
	})

	t.Run("Turn Count Limits", func(t *testing.T) {
		tests := []struct {
			turn     int
			contains []string
		}{
			{7, []string{"SYSTEM NOTICE", "3 turns remaining"}},
			{8, []string{"URGENT", "2 turns remain", "distilled state", "manage_history"}},
			{9, []string{"FINAL", "final turn", "forbidden"}},
		}
		for _, tt := range tests {
			got := cs.getTurnWarning(tt.turn)
			for _, want := range tt.contains {
				if !contains(got, want) {
					t.Errorf("expected turn %d warning to contain %q, got %q", tt.turn, want, got)
				}
			}
		}
	})
}

func TestContextStrategy_Warnings_InvalidStrategyConfig(t *testing.T) {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)

	t.Run("Zero Limits", func(t *testing.T) {
		cs.SetLimits(0, 0, 0)
		h, tool, hTurns := cs.GetLimits()
		if h <= 0 || tool <= 0 || hTurns <= 0 {
			t.Errorf("expected limits to remain positive defaults, got %d, %d, %d", h, tool, hTurns)
		}
	})
}

func TestContextStrategy_Warnings_SystemBufferExhaustion(t *testing.T) {
	cs := setupWarningTest()

	t.Run("History turn Warnings", func(t *testing.T) {
		tests := []struct {
			turns    int
			expected string
		}{
			{91, "90%"},
			{91, "manage_history"},
			{96, "95%"},
			{100, "limit has been reached"},
		}
		for _, tt := range tests {
			got := cs.getHistoryTurnWarning(tt.turns)
			if !contains(got, tt.expected) {
				t.Errorf("expected %d turns warning to contain %q, got %q", tt.turns, tt.expected, got)
			}
		}
	})

	t.Run("Pruning Counter Reset", func(t *testing.T) {
		cs.SetPrunedTurns(10)
		_ = cs.GetWarnings(1, 10, 1)
		if cs.prunedTurns != 0 {
			t.Errorf("expected prunedTurns to be reset, got %d", cs.prunedTurns)
		}
	})

	t.Run("Clogged Warning", func(t *testing.T) {
		w := cs.GetCloggedWarning()
		if !contains(w, "CRITICAL") || !contains(w, "summarization failed") {
			t.Errorf("expected clogged warning, got %q", w)
		}
	})
}

func TestContextStrategy_SetTieredThresholdZero(t *testing.T) {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)

	// First set to a non-zero value
	cs.SetTieredThreshold(100)
	if got := cs.GetTieredThreshold(); got != 100 {
		t.Errorf("expected tieredThreshold to be 100, got %d", got)
	}

	// Set threshold to 0 (disable)
	cs.SetTieredThreshold(0)
	if got := cs.GetTieredThreshold(); got != 0 {
		t.Errorf("expected tieredThreshold to be 0, got %d", got)
	}

	// Verify no price warning is generated when threshold is 0
	warnings := cs.GetWarnings(1, 200000, 1) // High token count
	for _, w := range warnings {
		if contains(w.Message, "ECONOMIC") {
			t.Errorf("expected no economic warnings when threshold is 0, but got: %q", w.Message)
		}
	}
}

func TestContextStrategy_GetPriceWarning(t *testing.T) {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}), nil)

	t.Run("Zero Threshold", func(t *testing.T) {
		cs.SetTieredThreshold(0)
		got := cs.getPriceWarningLocked(1000)
		if got != "" {
			t.Errorf("expected empty warning for zero threshold, got %q", got)
		}
	})

	t.Run("Below Warning", func(t *testing.T) {
		cs.SetTieredThreshold(1000)
		// WarningRatio is 0.78. 0.78 * 1000 = 780.
		got := cs.getPriceWarningLocked(500)
		if got != "" {
			t.Errorf("expected empty warning for 500 tokens (threshold 1000), got %q", got)
		}
	})

	t.Run("Warning Ratio", func(t *testing.T) {
		cs.SetTieredThreshold(1000)
		// 901 >= 780 (threshold * 0.78)
		got := cs.getPriceWarningLocked(901)
		if !contains(got, "[ECONOMIC NOTICE") {
			t.Errorf("expected [ECONOMIC NOTICE] prefix for 901 tokens (threshold 1000), got %q", got)
		}
	})

	t.Run("Threshold Hit", func(t *testing.T) {
		cs.SetTieredThreshold(1000)
		got := cs.getPriceWarningLocked(1001)
		if !contains(got, "[URGENT ECONOMIC NOTICE") {
			t.Errorf("expected [URGENT ECONOMIC NOTICE] prefix for 1001 tokens (threshold 1000), got %q", got)
		}
	})
}
