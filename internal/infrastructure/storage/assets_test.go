// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestAssetStore(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewAssetStore(tmpDir)
	ctx := context.Background()

	t.Run("Put and Get", func(t *testing.T) {
		data := []byte("hello world")
		id, err := store.Put(ctx, data)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		if id == "" {
			t.Fatal("expected non-empty ID")
		}

		got, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if !bytes.Equal(got, data) {
			t.Errorf("got %s, want %s", got, data)
		}
	})

	t.Run("Put empty data", func(t *testing.T) {
		id, err := store.Put(ctx, []byte{})
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %s", id)
		}
	})

	t.Run("Get empty ID", func(t *testing.T) {
		got, err := store.Get(ctx, "")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != nil {
			t.Error("expected nil data")
		}
	})

	t.Run("Put existing data", func(t *testing.T) {
		data := []byte("existing")
		id1, _ := store.Put(ctx, data)
		id2, err := store.Put(ctx, data)
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

	t.Run("Context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		data := []byte("canceled")
		_, err := store.Put(ctx, data)
		if err == nil {
			t.Error("expected error for canceled context, got nil")
		}

		_, err = store.Get(ctx, "any")
		if err == nil {
			t.Error("expected error for canceled context, got nil")
		}
	})
}

func TestAssetStore_WithFileSystem(t *testing.T) {
	tmpDir := t.TempDir()
	fs := &OSFileSystem{}
	store := NewAssetStore(tmpDir).WithFileSystem(fs)
	if store.fs != fs {
		t.Error("WithFileSystem failed to set filesystem")
	}
}
