// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMockFile(t *testing.T) {
	content := []byte("hello world")
	f := &MockFile{
		name:    "test.txt",
		content: content,
	}

	// Write should fail
	_, err := f.Write([]byte("foo"))
	if err == nil {
		t.Error("expected error on Write, got nil")
	}

	// Close should succeed
	err = f.Close()
	if err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
	if !f.closed {
		t.Error("expected closed to be true")
	}
}

func TestMockFileInfo(t *testing.T) {
	m := &MockFileInfo{
		name: "test.txt",
		size: 123,
		dir:  false,
	}

	if m.Name() != "test.txt" {
		t.Errorf("expected name test.txt, got %s", m.Name())
	}
	if m.Size() != 123 {
		t.Errorf("expected size 123, got %d", m.Size())
	}
	if m.Mode() != 0 {
		t.Errorf("expected mode 0, got %v", m.Mode())
	}
	if m.ModTime().IsZero() {
		t.Error("expected non-zero mod time")
	}
	if m.IsDir() {
		t.Error("expected IsDir to be false")
	}
	if m.Sys() != nil {
		t.Errorf("expected Sys nil, got %v", m.Sys())
	}
}

func TestMockFileSystem(t *testing.T) {
	ctx := context.Background()
	fs := NewMockFileSystem()

	// WriteFile
	data := []byte("content")
	err := fs.WriteFile(ctx, "dir/test.txt", data, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// ReadFile
	got, err := fs.ReadFile(ctx, "dir/test.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "content" {
		t.Errorf("expected content, got %s", string(got))
	}

	// ReadFile not exist
	_, err = fs.ReadFile(ctx, "nonexistent")
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got %v", err)
	}

	// ReadDir
	entries, err := fs.ReadDir(ctx, "dir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name() != "test.txt" {
		t.Errorf("expected test.txt, got %s", entries[0].Name())
	}
	if entries[0].IsDir() {
		t.Error("expected test.txt to not be a directory")
	}
	// Coverage for mockDirEntry methods
	_ = entries[0].Type()
	_, _ = entries[0].Info()

	// MkdirAll
	err = fs.MkdirAll(ctx, "a/b/c", 0755)
	if err != nil {
		t.Errorf("MkdirAll failed: %v", err)
	}

	// Stat file
	info, err := fs.Stat(ctx, "dir/test.txt")
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Name() != "test.txt" {
		t.Errorf("expected test.txt, got %s", info.Name())
	}

	// Stat dir
	info, err = fs.Stat(ctx, "dir")
	if err != nil {
		t.Fatalf("Stat dir failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected dir to be a directory")
	}

	// Stat not exist
	_, err = fs.Stat(ctx, "none")
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist, got %v", err)
	}

	// Open
	f, err := fs.Open(ctx, "dir/test.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// OpenFile
	f, err = fs.OpenFile(ctx, "dir/test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Remove
	err = fs.Remove(ctx, "dir/test.txt")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	_, err = fs.ReadFile(ctx, "dir/test.txt")
	if !os.IsNotExist(err) {
		t.Errorf("expected file to be removed")
	}
}

func TestMockFileSystem_Walk(t *testing.T) {
	ctx := context.Background()
	fs := NewMockFileSystem()

	if err := fs.WriteFile(ctx, "a/b/f1.txt", []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(ctx, "a/c/f2.txt", []byte("2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(ctx, "d/f3.txt", []byte("3"), 0644); err != nil {
		t.Fatal(err)
	}

	visited := make(map[string]bool)
	err := fs.Walk(ctx, "a", func(path string, info os.FileInfo, err error) error {
		visited[path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	expected := []string{"a", "a/b", "a/b/f1.txt", "a/c", "a/c/f2.txt"}
	for _, p := range expected {
		if !visited[filepath.Clean(p)] {
			t.Errorf("expected %s to be visited", p)
		}
	}
	if visited[filepath.Clean("d/f3.txt")] {
		t.Error("d/f3.txt should not be visited")
	}

	// Test SkipDir
	visited = make(map[string]bool)
	err = fs.Walk(ctx, "a", func(path string, info os.FileInfo, err error) error {
		visited[path] = true
		if path == filepath.Clean("a/b") {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk with SkipDir failed: %v", err)
	}
	if visited[filepath.Clean("a/b/f1.txt")] {
		t.Error("a/b/f1.txt should have been skipped")
	}
	if !visited[filepath.Clean("a/c/f2.txt")] {
		t.Error("a/c/f2.txt should have been visited")
	}

	// Test root = "."
	visited = make(map[string]bool)
	err = fs.Walk(ctx, ".", func(path string, info os.FileInfo, err error) error {
		visited[path] = true
		return nil
	})
	if err != nil {
		t.Fatalf("Walk . failed: %v", err)
	}
	if !visited[filepath.Clean("d/f3.txt")] {
		t.Error("d/f3.txt should be visited when root is .")
	}
}
