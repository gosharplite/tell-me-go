// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"strings"
	"testing"
)

func TestIsIgnoredDir(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"dot git", ".git", true},
		{"node_modules", "node_modules", true},
		{"vendor", "vendor", true},
		{"src", "src", false},
		{"internal", "internal", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIgnoredDir(tt.input); got != tt.expected {
				t.Errorf("isIgnoredDir(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatMatch(t *testing.T) {
	path := "test.txt"
	lineNum := 10
	text := "  some text  "
	
	got := formatMatch(path, lineNum, text)
	expected := "test.txt:10: some text"
	if got != expected {
		t.Errorf("formatMatch() = %q, want %q", got, expected)
	}

	// Test truncation
	longText := strings.Repeat("a", 600)
	got = formatMatch(path, lineNum, longText)
	if !strings.Contains(got, "(truncated)") {
		t.Error("expected truncation for long line")
	}
	if len(got) > 550 { // approximate
		t.Errorf("formatted match too long: %d", len(got))
	}
}
