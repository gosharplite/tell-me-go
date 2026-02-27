// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"
)

func TestTruncateSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{
			"Empty string",
			"",
			5,
			"",
		},
		{
			"Short string",
			"hello",
			10,
			"hello",
		},
		{
			"Exact length",
			"hello",
			5,
			"hello",
		},
		{
			"Truncated ASCII",
			"hello world",
			5,
			"hello...",
		},
		{
			"Truncated Unicode",
			"héllo",
			2,
			"hé...",
		},
		{
			"Multi-byte characters",
			"こんにちは",
			2,
			"こん...",
		},
		{
			"MaxRunes is zero",
			"hello",
			0,
			"...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateSafe([]byte(tt.input), tt.maxRunes)
			if got != tt.want {
				t.Errorf("truncateSafe() = %q, want %q", got, tt.want)
			}
		})
	}
}

func BenchmarkTruncateSafe(b *testing.B) {
	input := []byte("This is a relatively long string that we want to truncate to see if it allocates much memory.")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		truncateSafe(input, 10)
	}
}
