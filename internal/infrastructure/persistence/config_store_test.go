// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"
)

func TestConfigRepository(t *testing.T) {
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "config.json")
	repo := newConfigRepository(fs, file)

	t.Run("Save and Load Config", func(t *testing.T) {
		if err := repo.Set(ctx, "key", "val"); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.GetAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if loaded["key"] != "val" {
			t.Errorf("expected val, got %s", loaded["key"])
		}
	})

	t.Run("Load Non-existent File", func(t *testing.T) {
		repo2 := newConfigRepository(fs, filepath.Join(tempDir, "none.json"))
		loaded, err := repo2.GetAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 0 {
			t.Error("expected empty map for non-existent file")
		}
	})

	t.Run("Get and Delete", func(t *testing.T) {
		_ = repo.Set(ctx, "k2", "v2")
		val, _ := repo.Get(ctx, "k2")
		if val != "v2" {
			t.Errorf("expected v2, got %s", val)
		}

		if err := repo.Delete(ctx, "k2"); err != nil {
			t.Fatal(err)
		}
		val, _ = repo.Get(ctx, "k2")
		if val != "" {
			t.Error("expected empty string after delete")
		}
	})
}
