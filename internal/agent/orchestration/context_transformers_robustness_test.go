// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestGroupTurns_Robustness(t *testing.T) {
	tests := []struct {
		name     string
		history  []*llm.Content
		expected [][]string // Roles in each turn
	}{
		{
			name: "Standard interleaving",
			history: []*llm.Content{
				{Role: "user"}, {Role: "model"},
				{Role: "user"}, {Role: "model"},
			},
			expected: [][]string{{"user", "model"}, {"user", "model"}},
		},
		{
			name: "System at start",
			history: []*llm.Content{
				{Role: "system"},
				{Role: "user"}, {Role: "model"},
				{Role: "user"}, {Role: "model"},
			},
			expected: [][]string{{"system"}, {"user", "model"}, {"user", "model"}},
		},
		{
			name: "Consecutive users (tool calls)",
			history: []*llm.Content{
				{Role: "user"},
				{Role: "model"},
				{Role: "user"}, // Tool result
				{Role: "model"},
			},
			expected: [][]string{{"user", "model"}, {"user", "model"}},
		},
		{
			name: "Trailing user",
			history: []*llm.Content{
				{Role: "user"}, {Role: "model"},
				{Role: "user"},
			},
			expected: [][]string{{"user", "model"}, {"user"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns, err := groupTurns(context.Background(), tt.history)
			if err != nil {
				t.Fatalf("groupTurns failed: %v", err)
			}
			if len(turns) != len(tt.expected) {
				t.Fatalf("expected %d turns, got %d", len(tt.expected), len(turns))
			}
			for i, turn := range turns {
				if len(turn) != len(tt.expected[i]) {
					t.Errorf("turn %d: expected length %d, got %d", i, len(tt.expected[i]), len(turn))
					continue
				}
				for j, msg := range turn {
					if msg.Role != tt.expected[i][j] {
						t.Errorf("turn %d, msg %d: expected role %s, got %s", i, j, tt.expected[i][j], msg.Role)
					}
				}
			}
		})
	}
}

func TestIsTurnEmpty_ThoughtSignature(t *testing.T) {
	tests := []struct {
		name     string
		turn     []*llm.Content
		expected bool
	}{
		{
			name: "Empty with thought false",
			turn: []*llm.Content{
				{Parts: []*llm.Part{{IsThought: false}}},
			},
			expected: true,
		},
		{
			name: "Non-empty with thought true",
			turn: []*llm.Content{
				{Parts: []*llm.Part{{IsThought: true}}},
			},
			expected: false,
		},
		{
			name: "Non-empty with thought signature",
			turn: []*llm.Content{
				{Parts: []*llm.Part{{ThoughtSignature: []byte("sig")}}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := isTurnEmpty(tt.turn)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, got)
			}
		})
	}
}

func TestApplySummary_RoleAlternation(t *testing.T) {
	t.Run("Starts after system", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "system"},
			{Role: "user"}, {Role: "model"},
			{Role: "user"}, {Role: "model"},
		}
		// Summarize [U1, M1] (indices 1, 2)
		newHist := applySummaryToHistory(history, 1, 3, "summary")

		// Expected: [system, U_sum, M_sum, U2, M2]
		roles := []string{}
		for _, msg := range newHist {
			roles = append(roles, msg.Role)
		}
		expected := []string{"system", "user", "model", "user", "model"}
		for i, r := range roles {
			if i >= len(expected) || r != expected[i] {
				t.Errorf("unexpected roles: %v", roles)
				break
			}
		}
	})

	t.Run("Ends before model", func(t *testing.T) {
		history := []*llm.Content{
			{Role: "user"}, {Role: "model"},
			{Role: "user"}, {Role: "model"},
		}
		// Summarize [U1, M1] (indices 0, 2)
		newHist := applySummaryToHistory(history, 0, 2, "summary")

		// Expected: [U_sum, M_sum, U2, M2]
		roles := []string{}
		for _, msg := range newHist {
			roles = append(roles, msg.Role)
		}
		expected := []string{"user", "model", "user", "model"}
		for i, r := range roles {
			if i >= len(expected) || r != expected[i] {
				t.Errorf("unexpected roles: %v", roles)
				break
			}
		}
	})
}
