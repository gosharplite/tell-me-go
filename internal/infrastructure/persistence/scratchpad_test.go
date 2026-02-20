// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"

)

func TestScratchpadRepository(t *testing.T) {
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "scratchpad.md")
	repo := newScratchpadRepository(fs, file)

	t.Run("Save and Load Scratchpad", func(t *testing.T) {
		content := "Hello, world!"
		if err := repo.Set(ctx, "content", content); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.Get(ctx, "content")
		if err != nil {
			t.Fatal(err)
		}
		if loaded != content {
			t.Errorf("expected %s, got %s", content, loaded)
		}
	})

	t.Run("Load Non-existent File", func(t *testing.T) {
		repo2 := newScratchpadRepository(fs, filepath.Join(tempDir, "none.md"))
		loaded, err := repo2.Get(ctx, "content")
		if err != nil {
			t.Fatal(err)
		}
		if loaded != "" {
			t.Error("expected empty string for non-existent file")
		}
	})

	t.Run("Delete existing key", func(t *testing.T) {
		repo := newScratchpadRepository(fs, filepath.Join(tempDir, "delete_test.md"))
		err := repo.Set(ctx, "content", "initial data")
		if err != nil {
			t.Fatal(err)
		}
		
		err = repo.Delete(ctx, "content")
		if err != nil {
			t.Fatal(err)
		}
		
		loaded, err := repo.Get(ctx, "content")
		if err != nil {
			t.Fatal(err)
		}
		if loaded != "" {
			t.Errorf("expected empty string after delete, got %q", loaded)
		}
	})

	t.Run("Delete non-existent key fails gracefully", func(t *testing.T) {
		repo := newScratchpadRepository(fs, filepath.Join(tempDir, "delete_non_existent.md"))
		err := repo.Delete(ctx, "invalid_key")
		if err != nil {
			t.Errorf("expected no error deleting non-existent key, got %v", err)
		}
	})

	t.Run("GetAll correctly returns underlying map", func(t *testing.T) {
		repo := newScratchpadRepository(fs, filepath.Join(tempDir, "getall_test.md"))
		content := "all content here"
		err := repo.Set(ctx, "content", content)
		if err != nil {
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
	})
}
