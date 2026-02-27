// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package stringsutil

import "testing"

func TestTruncateOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		maxLines int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxLines: 5,
			expected: "",
		},
		{
			name:     "maxLines is zero",
			input:    "line 1\nline 2",
			maxLines: 0,
			expected: "\n... (Output truncated) ...",
		},
		{
			name:     "maxLines is negative",
			input:    "line 1\nline 2",
			maxLines: -1,
			expected: "\n... (Output truncated) ...",
		},
		{
			name:     "no truncation needed (maxLines > actual lines)",
			input:    "line 1\nline 2",
			maxLines: 5,
			expected: "line 1\nline 2",
		},
		{
			name:     "no truncation needed (maxLines == actual lines)",
			input:    "line 1\nline 2\nline 3",
			maxLines: 3,
			expected: "line 1\nline 2\nline 3",
		},
		{
			name:     "truncation needed (maxLines < actual lines)",
			input:    "line 1\nline 2\nline 3\nline 4",
			maxLines: 2,
			expected: "line 1\nline 2\n... (Output truncated) ...",
		},
		{
			name:     "no newlines, no truncation",
			input:    "single line with no newline",
			maxLines: 1,
			expected: "single line with no newline",
		},
		{
			name:     "no newlines, but maxLines is 0",
			input:    "single line",
			maxLines: 0,
			expected: "\n... (Output truncated) ...",
		},
		{
			name:     "ends with newline, no truncation",
			input:    "line 1\n",
			maxLines: 1,
			expected: "line 1\n",
		},
		{
			name:     "ends with newline, truncation",
			input:    "line 1\nline 2\n",
			maxLines: 1,
			expected: "line 1\n... (Output truncated) ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TruncateOutput(tt.input, tt.maxLines)
			if got != tt.expected {
				t.Errorf("TruncateOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}
