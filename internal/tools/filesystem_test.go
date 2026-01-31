// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

func TestReplaceText_Uniqueness(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tell-me-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")
	content := "line 1\ntarget\nline 3\ntarget\nline 5"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	sm.bypassConfirmations = true // Avoid interactive prompts

	m := &fileSystemManager{sm: sm, bm: NewBackupManager(sm, 1), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	// 1. Test failure when old_text appears multiple times
	args := map[string]interface{}{
		"filepath": filePath,
		"old_text": "target",
		"new_text": "replaced",
	}
	_, err = m.replaceText(ctx, args)
	if err == nil {
		t.Error("expected error when old_text is not unique, got nil")
	} else if !strings.Contains(err.Error(), "found 2 times") {
		t.Errorf("expected 'found 2 times' error, got: %v", err)
	}

	// 2. Test success when old_text is unique (with more context)
	args["old_text"] = "line 1\ntarget"
	_, err = m.replaceText(ctx, args)
	if err != nil {
		t.Errorf("expected success with context, got error: %v", err)
	}

	// Verify content
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "replaced\nline 3") {
		t.Errorf("content mismatch after replacement: %s", string(data))
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte{0}) {
		t.Error("expected isBinary to return true for data with null byte")
	}
	if isBinary([]byte("text")) {
		t.Error("expected isBinary to return false for plain text")
	}
}

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

	sm := NewSecurityManager()
	m := &fileSystemManager{sm: sm, fs: fsutil.DefaultFileSystem}

	ctx := context.Background()
	args := map[string]interface{}{
		"path":  tempDir,
		"query": "hello",
	}

	result, err := m.searchFiles(ctx, args)
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

func TestListFiles(t *testing.T) {
	tempDir := t.TempDir()
	os.WriteFile(filepath.Join(tempDir, "a.txt"), []byte("a"), 0644)
	os.Mkdir(filepath.Join(tempDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tempDir, "sub", "b.txt"), []byte("b"), 0644)

	sm := NewSecurityManager()
	m := &fileSystemManager{sm: sm, bm: NewBackupManager(sm, 1), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	t.Run("list root", func(t *testing.T) {
		res, err := m.listFiles(ctx, map[string]interface{}{"path": tempDir})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "[f] a.txt") || !strings.Contains(res.Text, "[d] sub") {
			t.Errorf("unexpected output: %s", res.Text)
		}
	})

	t.Run("non-existent path", func(t *testing.T) {
		_, err := m.listFiles(ctx, map[string]interface{}{"path": filepath.Join(tempDir, "missing")})
		if err == nil {
			t.Error("expected error for missing path")
		}
	})
}

func TestWriteFile(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSecurityManager()
	sm.bypassConfirmations = true
	m := &fileSystemManager{sm: sm, bm: NewBackupManager(sm, 1), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	path := filepath.Join(tempDir, "new.txt")
	content := "hello"
	_, err := m.writeFile(ctx, map[string]interface{}{
		"filepath": path,
		"content":  content,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("got %s, want %s", got, content)
	}
}

func TestReadFile(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	content := "some content"
	os.WriteFile(path, []byte(content), 0644)

	sm := NewSecurityManager()
	m := &fileSystemManager{sm: sm, bm: NewBackupManager(sm, 1), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	res, err := m.readFile(ctx, map[string]interface{}{"filepath": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, content) {
		t.Errorf("got %s, want %s", res.Text, content)
	}
}

func TestAppendText(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSecurityManager()
	sm.bypassConfirmations = true
	m := &fileSystemManager{sm: sm, bm: NewBackupManager(sm, 1), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	path := filepath.Join(tempDir, "append.txt")

	// Initial write (via append to new file)
	_, err := m.appendText(ctx, map[string]interface{}{
		"filepath": path,
		"content":  "line 1\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial content
	got, _ := os.ReadFile(path)
	if string(got) != "line 1\n" {
		t.Errorf("got %q, want %q", string(got), "line 1\n")
	}

	// Second append
	_, err = m.appendText(ctx, map[string]interface{}{
		"filepath": path,
		"content":  "line 2",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify appended content
	got, _ = os.ReadFile(path)
	expected := "line 1\nline 2"
	if string(got) != expected {
		t.Errorf("got %q, want %q", string(got), expected)
	}
}
