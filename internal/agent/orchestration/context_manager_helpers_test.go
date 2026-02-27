// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCalculateSummarizationEndIndex(t *testing.T) {
	turns := [][]*llm.Content{
		{{Role: "user", Parts: []*llm.Part{{Text: "u1"}}}, {Role: "model", Parts: []*llm.Part{{Text: "m1"}}}},
		{{Role: "user", Parts: []*llm.Part{{Text: "u2"}}}, {Role: "model", Parts: []*llm.Part{{Text: "m2"}}}},
		{{Role: "user", Parts: []*llm.Part{{Text: "u3"}}}, {Role: "model", Parts: []*llm.Part{{Text: "m3"}}}},
	}

	tests := []struct {
		name           string
		requestedTurns int
		expectedEndIdx int
		expectedTurns  int
	}{
		{"Normal", 2, 4, 2},
		{"TooMany", 5, 4, 2},
		{"All", 3, 4, 2},
		{"Zero", 0, 0, 0},
		{"Negative", -1, 0, 0},
		{"One", 1, 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endIdx, turnsUsed := calculateSummarizationEndIndex(turns, tt.requestedTurns)
			assert.Equal(t, tt.expectedEndIdx, endIdx)
			assert.Equal(t, tt.expectedTurns, turnsUsed)
		})
	}
}

func TestIsTokenCountSafe(t *testing.T) {
	tests := []struct {
		name     string
		tokens   int
		window   int
		expected bool
	}{
		{"Safe", 500, 1000, true},
		{"ExactlyLimit", 900, 1000, true},
		{"Unsafe", 901, 1000, false},
		{"Large", 10000, 100000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe, limit := isTokenCountSafe(tt.tokens, tt.window)
			assert.Equal(t, tt.expected, safe)
			assert.Equal(t, int(float64(tt.window)*0.9), limit)
		})
	}
}
