// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
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

	w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	// 1. Test failure when old_text appears multiple times
	args := map[string]interface{}{
		"filepath": filePath,
		"old_text": "target",
		"new_text": "replaced",
		"reason":   "testing",
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
	w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: infrapersistence.NewOSFileSystem()}
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
	w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: infrapersistence.NewOSFileSystem()}
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

type mockFS struct {
	persistence.FileSystem
	mkdirErr error
	writeErr error
}

func (m *mockFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if m.mkdirErr != nil {
		return m.mkdirErr
	}
	return os.MkdirAll(path, perm)
}

func (m *mockFS) WriteFile(ctx context.Context, filename string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	return os.WriteFile(filename, data, perm)
}

func TestWriteFile_Failures(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	sm.RegisterSafePath("/tmp")
	sm.RegisterSafePath("/private/tmp") // For macOS symlinks
	sm.RegisterSafePath("/mock")

	t.Run("mkdir failure", func(t *testing.T) {
		mfs := &mockFS{mkdirErr: fmt.Errorf("disk full")}
		w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: mfs}
		_, err := w.writeFile(context.Background(), map[string]interface{}{
			"filepath": "/mock/any/file.txt",
			"content":  "test",
			"reason":   "testing",
		})
		if err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Errorf("expected disk full error, got %v", err)
		}
	})

	t.Run("write failure", func(t *testing.T) {
		mfs := &mockFS{writeErr: fmt.Errorf("write error")}
		w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: mfs}
		tempDir := t.TempDir()
		sm.RegisterSafePath(tempDir)
		path := filepath.Join(tempDir, "file.txt")
		_, err := w.writeFile(context.Background(), map[string]interface{}{
			"filepath": path,
			"content":  "test",
			"reason":   "testing",
		})
		if err == nil || !strings.Contains(err.Error(), "write error") {
			t.Errorf("expected write error, got %v", err)
		}
	})
}

func TestUndoFileChange(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "undo.txt")
	content1 := "initial"
	content2 := "modified"
	if err := os.WriteFile(path, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	bm := newBackupManager(sm, 10)
	w := &fileWriter{sm: sm, bm: bm, fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	// Perform a write
	_, err := w.writeFile(ctx, map[string]interface{}{
		"filepath": path,
		"content":  content2,
		"reason":   "testing undo",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify change
	got, _ := os.ReadFile(path)
	if string(got) != content2 {
		t.Fatalf("expected %s, got %s", content2, string(got))
	}

	// Undo change
	res, err := w.undoFileChange(ctx, map[string]interface{}{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Undo successful") {
		t.Errorf("unexpected undo result: %s", res.Text)
	}

	// Verify revert
	got, _ = os.ReadFile(path)
	if string(got) != content1 {
		t.Errorf("after undo, expected %s, got %s", content1, string(got))
	}
}

func TestReplaceText_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: infrapersistence.NewOSFileSystem()}
	ctx := context.Background()

	_, err := w.replaceText(ctx, map[string]interface{}{
		"filepath": path,
		"old_text": "missing",
		"new_text": "new",
		"reason":   "testing",
	})
	if err == nil || !strings.Contains(err.Error(), "old_text not found") {
		t.Errorf("expected 'old_text not found' error, got %v", err)
	}
}

func TestAppendText_Failures(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	sm.RegisterSafePath("/tmp")
	sm.RegisterSafePath("/private/tmp")
	sm.RegisterSafePath("/mock")

	t.Run("open failure", func(t *testing.T) {
		mfs := &mockFS_Append{FileSystem: infrapersistence.NewOSFileSystem(), openErr: fmt.Errorf("open error")}
		w := &fileWriter{sm: sm, bm: newBackupManager(sm, 10), fs: mfs}
		_, err := w.appendText(context.Background(), map[string]interface{}{
			"filepath": "/mock/any.txt",
			"content":  "test",
			"reason":   "testing",
		})
		if err == nil || !strings.Contains(err.Error(), "open error") {
			t.Errorf("expected open error, got %v", err)
		}
	})
}

type mockFS_Append struct {
	persistence.FileSystem
	openErr error
}

func (m *mockFS_Append) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	return nil, m.openErr
}
