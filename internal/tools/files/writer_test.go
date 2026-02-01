// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
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

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tmpDir)
	sm.SetBypassActive(true) // Avoid interactive prompts

	w := &fileWriter{sm: sm, bm: NewBackupManager(sm, 10), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	// 1. Test failure when old_text appears multiple times
	args := map[string]interface{}{
		"filepath": filePath,
		"old_text": "target",
		"new_text": "replaced",
		"reason": "testing",
	}
	_, err = w.replaceText(ctx, args)
	if err == nil {
		t.Error("expected error when old_text is not unique, got nil")
	} else if !strings.Contains(err.Error(), "found 2 times") {
		t.Errorf("expected 'found 2 times' error, got: %v", err)
	}

	// 2. Test success when old_text is unique (with more context)
	args["old_text"] = "line 1\ntarget"
	_, err = w.replaceText(ctx, args)
	if err != nil {
		t.Errorf("expected success with context, got error: %v", err)
	}

	// Verify content
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "replaced\nline 3") {
		t.Errorf("content mismatch after replacement: %s", string(data))
	}
}

func TestWriteFile(t *testing.T) {
	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	w := &fileWriter{sm: sm, bm: NewBackupManager(sm, 10), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	path := filepath.Join(tempDir, "new.txt")
	content := "hello"
	_, err := w.writeFile(ctx, map[string]interface{}{
		"filepath": path,
		"content":  content,
		"reason":   "testing",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("got %s, want %s", got, content)
	}
}

func TestAppendText(t *testing.T) {
	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	w := &fileWriter{sm: sm, bm: NewBackupManager(sm, 10), fs: fsutil.DefaultFileSystem}
	ctx := context.Background()

	path := filepath.Join(tempDir, "append.txt")

	// Initial write (via append to new file)
	_, err := w.appendText(ctx, map[string]interface{}{
		"filepath": path,
		"content":  "line 1\n",
		"reason":   "testing",
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
	_, err = w.appendText(ctx, map[string]interface{}{
		"filepath": path,
		"content":  "line 2",
		"reason":   "testing",
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
