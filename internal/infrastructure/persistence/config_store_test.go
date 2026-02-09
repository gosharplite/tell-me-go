// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestConfigStore(t *testing.T) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem

	t.Run("Set and Get Config", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		store := NewConfigStore(fs, configFile)

		_, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "set",
			"key":    "theme",
			"value":  "dark",
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "get",
			"key":    "theme",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "dark" {
			t.Errorf("expected dark, got %s", res.Text)
		}
	})

	t.Run("Delete Key", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		store := NewConfigStore(fs, configFile)

		if _, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "set",
			"key":    "theme",
			"value":  "dark",
		}); err != nil {
			t.Fatal(err)
		}

		_, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "delete",
			"key":    "theme",
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.ManageConfig(ctx, map[string]interface{}{
			"action": "get",
			"key":    "theme",
		})
		if err == nil {
			t.Fatal("expected error for deleted key")
		}
	})

	t.Run("Delete Missing Key", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		store := NewConfigStore(fs, configFile)

		// Should not return error, just delete nothing
		_, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "delete",
			"key":    "missing",
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Persistence", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		store1 := NewConfigStore(fs, configFile)

		if _, err := store1.ManageConfig(ctx, map[string]interface{}{
			"action": "set",
			"key":    "theme",
			"value":  "dark",
		}); err != nil {
			t.Fatal(err)
		}

		store2 := NewConfigStore(fs, configFile)
		err := store2.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}

		res, err := store2.ManageConfig(ctx, map[string]interface{}{
			"action": "get",
			"key":    "theme",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "dark" {
			t.Error("config was not persisted")
		}
	})

	t.Run("List Config", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		store := NewConfigStore(fs, configFile)

		if _, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "set",
			"key":    "k1",
			"value":  "v1",
		}); err != nil {
			t.Fatal(err)
		}

		res, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "list",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "k1 = v1") {
			t.Errorf("expected k1 = v1 in list, got %s", res.Text)
		}
	})

	t.Run("List Empty Config", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		store := NewConfigStore(fs, configFile)

		res, err := store.ManageConfig(ctx, map[string]interface{}{
			"action": "list",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "Configuration is empty") {
			t.Errorf("expected empty message, got %s", res.Text)
		}
	})

	t.Run("Corrupted JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.json")
		if err := fs.WriteFile(ctx, configFile, []byte("invalid json"), 0644); err != nil {
			t.Fatal(err)
		}

		store := NewConfigStore(fs, configFile)
		err := store.Load(ctx)
		if err == nil {
			t.Fatal("expected error for corrupted JSON")
		}
	})
}
