// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestConfigRepository(t *testing.T) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "config.json")
	repo := NewConfigRepository(fs, file)

	t.Run("Save and Load Config", func(t *testing.T) {
		config := map[string]string{"key": "val"}
		if err := repo.SaveConfig(ctx, config); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.LoadConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if loaded["key"] != "val" {
			t.Errorf("expected val, got %s", loaded["key"])
		}
	})

	t.Run("Load Non-existent File", func(t *testing.T) {
		repo2 := NewConfigRepository(fs, filepath.Join(tempDir, "none.json"))
		loaded, err := repo2.LoadConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 0 {
			t.Error("expected empty map for non-existent file")
		}
	})
}
