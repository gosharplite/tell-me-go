// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"testing"
)

func TestShouldIgnoreDir(t *testing.T) {
	policy := NewWorkspacePolicy()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Canonical ignored directory names (union of all pre-refactor lists)
		{".git", ".git", true},
		{"node_modules", "node_modules", true},
		{"vendor", "vendor", true},
		{"bin", "bin", true},
		{"obj", "obj", true},
		{"output", "output", true},
		{"dist", "dist", true},
		{"testdata", "testdata", true},
		{"configs", "configs", true},

		// Hidden directory heuristic
		{"hidden dir", ".hidden", true},
		{"vscode", ".vscode", true},

		// Edge cases: . and .. must not be ignored
		{"current dir dot", ".", false},
		{"parent dir double dot", "..", false},

		// Normal directories that must not be ignored
		{"src", "src", false},
		{"internal", "internal", false},
		{"pkg", "pkg", false},
		{"cmd", "cmd", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.ShouldIgnoreDir(tt.input); got != tt.expected {
				t.Errorf("ShouldIgnoreDir(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestShouldIgnorePath(t *testing.T) {
	policy := NewWorkspacePolicy()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		// Normal files — not ignored
		{"normal go file", "main.go", false},
		{"normal dir", "internal/pkg/handler.go", false},

		// Directory names in path trigger ignore
		{"git dir at root", ".git/config", true},
		{"vendor in path", "vendor/pkg/foo.go", true},
		{"node_modules in path", "node_modules/pkg/foo.js", true},
		{"configs at root", "configs/architect.yaml", true},

		// Ignored component in middle of path
		{"configs nested", "internal/configs/foo.yaml", true},
		{"git nested mid-path", "src/.git/refs/heads/main", true},
		{"vendor deep", "a/b/vendor/c/d/file.go", true},

		// File extension exclusions
		{"test file", "foo_test.go", true},
		{"markdown", "README.md", true},
		{"json file", "data.json", true},
		{"golden file", "test.golden", true},

		// Edge cases
		{"empty path", "", false},
		{"dot only", ".", false},
		{"file in hidden dir", ".hidden/file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.ShouldIgnorePath(tt.path); got != tt.expected {
				t.Errorf("ShouldIgnorePath(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}
