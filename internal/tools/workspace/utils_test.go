// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
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

func TestWalkAndProcess(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "safe"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "safe/f1.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.RegisterSafePath(tempDir)
	sm.IsSafeFunc = func(path string) (string, error) {
		if strings.HasPrefix(path, tempDir) {
			return path, nil
		}
		return "", os.ErrPermission
	}

	ctx := context.Background()
	var seen []string
	processor := func(path string) error {
		seen = append(seen, filepath.Base(path))
		return nil
	}

	t.Run("safe path", func(t *testing.T) {
		err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), tempDir, nil, processor)
		if err != nil {
			t.Fatal(err)
		}
		if len(seen) != 1 || seen[0] != "f1.txt" {
			t.Errorf("unexpected files seen: %v", seen)
		}
	})

	t.Run("unsafe path", func(t *testing.T) {
		err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), "/etc", nil, processor)
		if err == nil {
			t.Error("expected error for unsafe path")
		}
	})
}
