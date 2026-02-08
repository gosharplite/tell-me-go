// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestSearchFiles_SkipsBinary(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "search_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	binaryPath := filepath.Join(tempDir, "binary.bin")
	err = os.WriteFile(binaryPath, []byte{0, 1, 2, 3}, 0644)
	if err != nil {
		t.Fatal(err)
	}

	textPath := filepath.Join(tempDir, "text.txt")
	err = os.WriteFile(textPath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	s := &fileSearcher{sm: sm, fs: fsutil.DefaultFileSystem}

	ctx := context.Background()
	args := map[string]interface{}{
		"path":  tempDir,
		"query": "hello",
	}

	result, err := s.searchFiles(ctx, args)
	if err != nil {
		t.Fatalf("searchFiles failed: %v", err)
	}

	if !strings.Contains(result.Text, "text.txt:1: hello world") {
		t.Errorf("expected result to contain text file match, got %q", result.Text)
	}

	if strings.Contains(result.Text, "binary.bin") {
		t.Error("expected result NOT to contain binary file match")
	}
}

func TestGrepDefinitions(t *testing.T) {
	tempDir := t.TempDir()

	pyFile := filepath.Join(tempDir, "script.py")
	if err := os.WriteFile(pyFile, []byte("def my_func():\n    pass\nclass MyClass:\n    pass"), 0644); err != nil {
		t.Fatal(err)
	}

	jsFile := filepath.Join(tempDir, "script.js")
	if err := os.WriteFile(jsFile, []byte("function jsFunc() {}\nconst arrow = () => {}"), 0644); err != nil {
		t.Fatal(err)
	}

	goFile := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(goFile, []byte("func main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	s := &fileSearcher{sm: sm, fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	t.Run("grep all definitions", func(t *testing.T) {
		res, err := s.grepDefinitions(ctx, map[string]interface{}{"path": tempDir})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "def my_func") ||
			!strings.Contains(res.Text, "class MyClass") ||
			!strings.Contains(res.Text, "function jsFunc") ||
			!strings.Contains(res.Text, "const arrow") {
			t.Errorf("expected definitions not found: %s", res.Text)
		}
	})

	t.Run("grep with query", func(t *testing.T) {
		res, err := s.grepDefinitions(ctx, map[string]interface{}{"path": tempDir, "query": "my_func"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "def my_func") || strings.Contains(res.Text, "jsFunc") {
			t.Errorf("unexpected results with query: %s", res.Text)
		}
	})

	t.Run("no results", func(t *testing.T) {
		res, err := s.grepDefinitions(ctx, map[string]interface{}{"path": tempDir, "query": "nonexistent"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "No definitions found." {
			t.Errorf("expected 'No definitions found.', got %q", res.Text)
		}
	})
}

func TestSearchFiles_TooManyResults(t *testing.T) {
	tempDir := t.TempDir()
	// The limit in searchFiles is hardcoded to 100.
	// We need 101 matches to trigger truncation.
	for i := 0; i < 101; i++ {
		if err := os.WriteFile(filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i)), []byte("match"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	sm := security.NewSecurityManager(nil)
	s := &fileSearcher{sm: sm, fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	args := map[string]interface{}{
		"path":  tempDir,
		"query": "match",
	}

	res, err := s.searchFiles(ctx, args)
	if err != nil {
		t.Fatalf("searchFiles failed: %v", err)
	}

	if !strings.Contains(res.Text, "... (truncated)") {
		t.Error("expected truncation message in results")
	}

	lines := strings.Split(strings.TrimSpace(res.Text), "\n")
	// Last line is truncation message, so we expect 101 lines if it includes the message as a line or similar.
	// Actually strings.Join(results, "\n") + "\n... (truncated)"
	if len(lines) < 100 {
		t.Errorf("expected at least 100 results before truncation, got %d", len(lines))
	}
}
