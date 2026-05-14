// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// translateRM tests
// ---------------------------------------------------------------------------

func TestWindowsTranslator_translateRM(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "recursive flag (-r)",
			input:    []string{"-r", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "combined recursive+force flag (-rf)",
			input:    []string{"-rf", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "force-only flag (non-recursive)",
			input:    []string{"-f", "file.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "file.txt"},
		},
		{
			name:     "verbose flag stripped (non-recursive)",
			input:    []string{"-v", "file.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "file.txt"},
		},
		{
			name:     "no flags, multiple files",
			input:    []string{"file1.txt", "file2.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "file1.txt", "file2.txt"},
		},
		{
			name:     "empty args (edge case)",
			input:    []string{},
			expected: []string{"cmd", "/c", "del", "/f", "/q"},
		},
		{
			name:     "separate flags, recursive wins",
			input:    []string{"-r", "-f", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "reverse flag order, recursive wins",
			input:    []string{"-f", "-r", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "unknown flags pass through as paths",
			input:    []string{"-x", "something"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "-x", "something"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.translateRM(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("translateRM() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// translateMkdir tests
// ---------------------------------------------------------------------------

func TestWindowsTranslator_translateMkdir(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "strip -p flag",
			input:    []string{"-p", "a/b/c"},
			expected: []string{"cmd", "/c", "mkdir", "a/b/c"},
		},
		{
			name:     "only -p flag",
			input:    []string{"-p"},
			expected: []string{"cmd", "/c", "mkdir"},
		},
		{
			name:     "no flags",
			input:    []string{"dir"},
			expected: []string{"cmd", "/c", "mkdir", "dir"},
		},
		{
			name:     "empty args",
			input:    []string{},
			expected: []string{"cmd", "/c", "mkdir"},
		},
		{
			name:     "multiple directories with -p",
			input:    []string{"-p", "a/b/c", "d/e/f"},
			expected: []string{"cmd", "/c", "mkdir", "a/b/c", "d/e/f"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.translateMkdir(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("translateMkdir() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// translateCP tests
// ---------------------------------------------------------------------------

func TestWindowsTranslator_translateCP(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "src and dst",
			input:    []string{"src.txt", "dst.txt"},
			expected: []string{"cmd", "/c", "copy", "src.txt", "dst.txt"},
		},
		{
			name:     "single arg",
			input:    []string{"file.txt"},
			expected: []string{"cmd", "/c", "copy", "file.txt"},
		},
		{
			name:     "empty args",
			input:    []string{},
			expected: []string{"cmd", "/c", "copy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.translateCP(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("translateCP() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// translateMV tests
// ---------------------------------------------------------------------------

func TestWindowsTranslator_translateMV(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "src and dst",
			input:    []string{"src.txt", "dst.txt"},
			expected: []string{"cmd", "/c", "move", "src.txt", "dst.txt"},
		},
		{
			name:     "single arg",
			input:    []string{"file.txt"},
			expected: []string{"cmd", "/c", "move", "file.txt"},
		},
		{
			name:     "empty args",
			input:    []string{},
			expected: []string{"cmd", "/c", "move"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.translateMV(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("translateMV() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// posixTranslator.Translate tests
// ---------------------------------------------------------------------------

func TestPosixTranslator_Translate(t *testing.T) {
	p := &posixTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "normal command",
			input:    []string{"ls", "-la"},
			expected: []string{"ls", "-la"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Translate(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Translate() = %v, want %v", got, tt.expected)
			}
		})
	}
}
