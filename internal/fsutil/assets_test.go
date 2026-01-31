// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package fsutil

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestAssetStore(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewAssetStore(tmpDir)

	t.Run("Put and Get", func(t *testing.T) {
		data := []byte("hello world")
		id, err := store.Put(data)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		if id == "" {
			t.Fatal("expected non-empty ID")
		}

		got, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if !bytes.Equal(got, data) {
			t.Errorf("got %s, want %s", got, data)
		}
	})

	t.Run("Put empty data", func(t *testing.T) {
		id, err := store.Put([]byte{})
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %s", id)
		}
	})

	t.Run("Get empty ID", func(t *testing.T) {
		got, err := store.Get("")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != nil {
			t.Error("expected nil data")
		}
	})

	t.Run("Put existing data", func(t *testing.T) {
		data := []byte("existing")
		id1, _ := store.Put(data)
		id2, err := store.Put(data)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if id1 != id2 {
			t.Errorf("expected same ID, got %s and %s", id1, id2)
		}
	})

	t.Run("GetPath short ID", func(t *testing.T) {
		path := store.GetPath("a")
		expected := filepath.Join(tmpDir, "a")
		if path != expected {
			t.Errorf("got %s, want %s", path, expected)
		}
	})
}
