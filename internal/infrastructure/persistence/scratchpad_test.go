// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestScratchpadRepository(t *testing.T) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "scratchpad.md")
	repo := NewScratchpadRepository(fs, file)

	t.Run("Save and Load Scratchpad", func(t *testing.T) {
		content := "Hello, world!"
		if err := repo.SaveScratchpad(ctx, content); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.LoadScratchpad(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if loaded != content {
			t.Errorf("expected %s, got %s", content, loaded)
		}
	})

	t.Run("Load Non-existent File", func(t *testing.T) {
		repo2 := NewScratchpadRepository(fs, filepath.Join(tempDir, "none.md"))
		loaded, err := repo2.LoadScratchpad(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if loaded != "" {
			t.Error("expected empty string for non-existent file")
		}
	})
}
