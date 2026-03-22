// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestContextStrategy_estimateTokens(t *testing.T) {
	registry := &mockToolRegistry{}
	cs := NewContextStrategy(NewHeuristicTokenCounter(registry))

	t.Run("Base overhead", func(t *testing.T) {
		registry.declarations = nil
		// base = 300
		got := cs.estimateTokens(nil)
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
		got := cs.estimateTokens(nil)
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
		base := cs.estimateTokens(nil)
		withBlob := cs.estimateTokens(contents)
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
		got := cs.estimateTokens(contents)
		if got != 309 {
			t.Errorf("expected 309 tokens, got %d", got)
		}
	})
}

func setupWarningTest() *ContextStrategy {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}))
	cs.SetLimits(1000, 10, 100)
	return cs
}

func TestContextStrategy_Warnings_WarningVerification(t *testing.T) {
	cs := setupWarningTest()
	warnings := cs.getWarnings(0, 0, 0, 0)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty history, got %d", len(warnings))
	}
}

func TestContextStrategy_Warnings_TokenPressureValidation(t *testing.T) {
	cs := setupWarningTest()

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
		warnings := cs.getWarnings(0, tt.tokens, 0, 0)
		got := ""
		for _, w := range warnings {
			if tt.expected != "" && contains(w.Message, tt.expected) {
				got = w.Message
				break
			}
		}
		if tt.expected == "" {
			if len(warnings) != 0 {
				t.Errorf("expected no warning at %d, got %q", tt.tokens, warnings[0].Message)
			}
		} else if got == "" {
			t.Errorf("expected warning at %d to contain %q, got none", tt.tokens, tt.expected)
		}
	}
}

func TestContextStrategy_Warnings_TurnCountLimits(t *testing.T) {
	cs := setupWarningTest()

	tests := []struct {
		turn     int
		contains []string
	}{
		{7, []string{"SYSTEM NOTICE", "3 turns remaining"}},
		{8, []string{"URGENT", "2 turns remain", "distilled state", "manage_history"}},
		{9, []string{"FINAL", "final turn", "forbidden"}},
	}
	for _, tt := range tests {
		warnings := cs.getWarnings(tt.turn, 0, 0, 0)
		got := ""
		if len(warnings) > 0 {
			got = warnings[0].Message
		}
		for _, want := range tt.contains {
			if !contains(got, want) {
				t.Errorf("expected turn %d warning to contain %q, got %q", tt.turn, want, got)
			}
		}
	}
}

func TestContextStrategy_Warnings_InvalidStrategyConfig(t *testing.T) {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}))

	t.Run("Zero Limits", func(t *testing.T) {
		cs.SetLimits(0, 0, 0)
		h, tool, hTurns := cs.getLimits()
		if h <= 0 || tool <= 0 || hTurns < 0 {
			t.Errorf("expected limits to remain positive/zero defaults, got %d, %d, %d", h, tool, hTurns)
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
			{100, ""},
		}
		for _, tt := range tests {
			warnings := cs.getWarnings(0, 0, tt.turns, 0)
			if tt.expected == "" {
				if len(warnings) > 0 {
					for _, w := range warnings {
						if contains(w.Message, "limit has been reached") {
							t.Errorf("expected no warning for %d turns, got %v", tt.turns, w.Message)
						}
					}
				}
				continue
			}

			found := false
			for _, w := range warnings {
				if contains(w.Message, tt.expected) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %d turns warning to contain %q, got %v", tt.turns, tt.expected, warnings)
			}
		}
	})

	t.Run("Pruning Counter Reset", func(t *testing.T) {
		// Since we no longer use internal state for prunedTurns, we just check that it's passed through

		warnings := cs.getWarnings(1, 10, 1, 10)
		found := false
		for _, w := range warnings {
			if contains(w.Message, "major history cleanup") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected cleanup warning when prunedTurns=10")
		}
	})

	t.Run("Clogged Warning", func(t *testing.T) {
		w := cs.getCloggedWarning()
		if !contains(w, "CRITICAL") || !contains(w, "summarization failed") {
			t.Errorf("expected clogged warning, got %q", w)
		}
	})
}

func TestContextStrategy_setTieredThresholdZero(t *testing.T) {
	cs := NewContextStrategy(NewHeuristicTokenCounter(&mockToolRegistry{}))

	// First set to a non-zero value
	cs.setTieredThreshold(100)
	if got := cs.GetTieredThreshold(); got != 100 {
		t.Errorf("expected tieredThreshold to be 100, got %d", got)
	}

	// Set threshold to 0 (disable)
	cs.setTieredThreshold(0)
	if got := cs.GetTieredThreshold(); got != 0 {
		t.Errorf("expected tieredThreshold to be 0, got %d", got)
	}

	// Verify no price warning is generated when threshold is 0
	warnings := cs.getWarnings(1, 200000, 1, 0) // High token count
	for _, w := range warnings {
		if contains(w.Message, "ECONOMIC") {
			t.Errorf("expected no economic warnings when threshold is 0, but got: %q", w.Message)
		}
	}
}

func TestContextStrategy_getPriceWarningLocked(t *testing.T) {
	// cliff = tieredThreshold
	// warning = int(float64(cliff) * config.WarningRatio)

	tests := []struct {
		name      string
		threshold int
		tokens    int
		want      string // expected substring
	}{
		{
			name:      "Threshold disabled (0)",
			threshold: 0,
			tokens:    1000000,
			want:      "",
		},
		{
			name:      "Well below warning threshold",
			threshold: 1000,
			tokens:    500,
			want:      "",
		},
		{
			name:      "Exactly at warning threshold (780 for 1000)",
			threshold: 1000,
			tokens:    780,
			want:      "[ECONOMIC NOTICE",
		},
		{
			name:      "Just below limit",
			threshold: 1000,
			tokens:    999,
			want:      "[ECONOMIC NOTICE",
		},
		{
			name:      "Exactly at limit",
			threshold: 1000,
			tokens:    1000,
			want:      "[URGENT ECONOMIC NOTICE",
		},
		{
			name:      "Over limit",
			threshold: 1000,
			tokens:    1001,
			want:      "[URGENT ECONOMIC NOTICE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange

			cs := &ContextStrategy{
				tieredThreshold: tt.threshold,
			}

			// Act
			got := cs.getPriceWarningLocked(tt.tokens)

			// Assert
			if tt.want == "" {
				if got != "" {
					t.Errorf("expected empty warning, got %q", got)
				}
			} else {
				if !strings.Contains(got, tt.want) {
					t.Errorf("expected warning to contain %q, got %q", tt.want, got)
				}
			}
		})
	}
}

func TestContextStrategy_getHistoryTurnWarningLocked(t *testing.T) {
	tests := []struct {
		name         string
		maxTurns     int
		prunedTurns  int
		currentTurns int
		wantContains string
	}{
		{
			name:         "Disabled: Max turns 0",
			maxTurns:     0,
			currentTurns: 100,
			wantContains: "",
		},
		{
			name:         "Well below limit (80%)",
			maxTurns:     100,
			currentTurns: 80,
			wantContains: "",
		},
		{
			name:         "Exactly at 90% threshold (ratio 0.90 is not > 0.90)",
			maxTurns:     100,
			currentTurns: 90,
			wantContains: "",
		},
		{
			name:         "Just over 90% threshold",
			maxTurns:     100,
			currentTurns: 91,
			wantContains: "90% of the turn limit",
		},
		{
			name:         "At 95% threshold (ratio 0.95 is not > 0.95)",
			maxTurns:     100,
			currentTurns: 95,
			wantContains: "90% of the turn limit",
		},
		{
			name:         "Just over 95% threshold",
			maxTurns:     100,
			currentTurns: 96,
			wantContains: "95% of the turn limit",
		},
		{
			name:         "At limit (100%)",
			maxTurns:     100,
			currentTurns: 100,
			wantContains: "",
		},
		{
			name:         "Over limit",
			maxTurns:     100,
			currentTurns: 105,
			wantContains: "",
		},
		{
			name:         "Major cleanup (pruned > 5)",
			maxTurns:     100,
			prunedTurns:  10,
			currentTurns: 1,
			wantContains: "major history cleanup has occurred",
		},
		{
			name:         "Minor cleanup (pruned <= 5)",
			maxTurns:     100,
			prunedTurns:  5,
			currentTurns: 1,
			wantContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange

			cs := &ContextStrategy{
				maxHistoryTurns: tt.maxTurns,
			}

			// Act
			got := cs.getHistoryTurnWarningLocked(tt.currentTurns, tt.prunedTurns)

			// Assert
			if tt.wantContains == "" {
				if got != "" {
					t.Errorf("expected empty warning, got %q", got)
				}
			} else {
				if !strings.Contains(got, tt.wantContains) {
					t.Errorf("expected warning to contain %q, got %q", tt.wantContains, got)
				}
			}
		})
	}
}

func TestContextStrategy_Count(t *testing.T) {
	mockCounter := &mockTokenCounter{tokens: 42}
	cs := NewContextStrategy(mockCounter)

	contents := []*llm.Content{{Parts: []*llm.Part{{Text: "hello"}}}}
	got := cs.Count(contents)

	assert.Equal(t, 42, got)
}
