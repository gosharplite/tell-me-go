// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"testing"
)

func TestCalculateVisualLines(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  int
	}{
		{"empty", "", 80, 0},
		{"short", "hello", 80, 1},
		{"exact wrap", "12345", 5, 1},
		{"wrap over", "123456", 5, 2},
		{"newlines", "a\nb\nc", 80, 3},
		{"zero width fallback", "abc", 0, 1},
	}
	r := &StdUIRenderer{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.calculateVisualLines(tt.text, tt.width); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func FuzzCalculateVisualLines(f *testing.F) {
	f.Add("standard text sample", 80)
	f.Fuzz(func(t *testing.T, text string, width int) {
		r := &StdUIRenderer{}
		// Ensure it never panics regardless of input or width
		_ = r.calculateVisualLines(text, width)
	})
}
