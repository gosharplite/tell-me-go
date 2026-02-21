// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"
)

func TestScratchpadRepository_Load(t *testing.T) {
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "scratchpad.md")
	repo := newScratchpadRepository(fs, file)

	content := "Hello, world!"
	if err := fs.WriteFile(ctx, file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.Get(ctx, "content")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != content {
		t.Errorf("expected %s, got %s", content, loaded)
	}
}

func TestScratchpadRepository_LoadNonExistent(t *testing.T) {
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()

	repo2 := newScratchpadRepository(fs, filepath.Join(tempDir, "none.md"))
	loaded, err := repo2.Get(ctx, "content")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != "" {
		t.Error("expected empty string for non-existent file")
	}
}

func TestScratchpadRepository_GetAll(t *testing.T) {
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()

	repo := newScratchpadRepository(fs, filepath.Join(tempDir, "getall_test.md"))
	content := "all content here"
	if err := fs.WriteFile(ctx, filepath.Join(tempDir, "getall_test.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 1 {
		t.Fatalf("expected length 1, got %d", len(all))
	}
	if all["content"] != content {
		t.Errorf("expected content %q, got %q", content, all["content"])
	}
}
