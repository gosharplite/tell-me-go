// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestAssetStore(t *testing.T) {
	t.Parallel()
	t.Run("SaveAndLoad", testSaveAndLoad)
	t.Run("Metadata", testMetadata)
	t.Run("PathSanitization", testPathSanitization)
	t.Run("MissingAssets", testMissingAssets)
	t.Run("Context", testContext)
}

func testSaveAndLoad(t *testing.T) {
	store := NewAssetStore(t.TempDir())
	ctx := context.Background()

	assertContent := func(t *testing.T, got, want []byte) {
		t.Helper()
		if !bytes.Equal(got, want) {
			t.Errorf("got %s, want %s", got, want)
		}
	}

	t.Run("Basic", func(t *testing.T) {
		data := []byte("hello world")
		id := createTestAsset(t, store, data)
		if id == "" {
			t.Fatal("expected non-empty ID")
		}

		got, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		assertContent(t, got, data)
	})

	t.Run("EmptyData", func(t *testing.T) {
		id, err := store.Put(ctx, []byte{})
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if id != "" {
			t.Errorf("expected empty ID, got %s", id)
		}
	})

	t.Run("Deduplication", func(t *testing.T) {
		data := []byte("existing")
		id1 := createTestAsset(t, store, data)
		id2 := createTestAsset(t, store, data)
		if id1 != id2 {
			t.Errorf("expected same ID, got %s and %s", id1, id2)
		}
	})
}

func testMetadata(t *testing.T) {
	store := NewAssetStore(t.TempDir())
	ctx := context.Background()
	content := []byte("metadata test content")
	id := createTestAsset(t, store, content)

	path := store.getPath(id)
	info, err := store.fs.Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	t.Run("Size", func(t *testing.T) {
		if info.Size() != int64(len(content)) {
			t.Errorf("got size %d, want %d", info.Size(), len(content))
		}
	})

	t.Run("Timestamp", func(t *testing.T) {
		if info.ModTime().IsZero() {
			t.Error("expected non-zero modification time")
		}
	})
}

func testPathSanitization(t *testing.T) {
	store := NewAssetStore(t.TempDir())

	t.Run("ShortID", func(t *testing.T) {
		path := store.getPath("a")
		if filepath.Base(path) != "a" {
			t.Errorf("expected filename 'a', got %s", filepath.Base(path))
		}
	})

	t.Run("LongID", func(t *testing.T) {
		id := "abcdef123456"
		path := store.getPath(id)
		expectedSuffix := filepath.Join("ab", id)
		// Check that it ends with ab/abcdef123456
		if !bytes.HasSuffix([]byte(path), []byte(expectedSuffix)) {
			t.Errorf("expected path to end with %s, got %s", expectedSuffix, path)
		}
	})
}

func testMissingAssets(t *testing.T) {
	store := NewAssetStore(t.TempDir())
	ctx := context.Background()

	assertNil := func(t *testing.T, got []byte) {
		t.Helper()
		if got != nil {
			t.Error("expected nil data")
		}
	}

	t.Run("EmptyID", func(t *testing.T) {
		got, err := store.Get(ctx, "")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		assertNil(t, got)
	})

	t.Run("NonExistent", func(t *testing.T) {
		got, err := store.Get(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent asset, got nil")
		}
		assertNil(t, got)
	})
}

func testContext(t *testing.T) {
	store := NewAssetStore(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("PutCanceled", func(t *testing.T) {
		_, err := store.Put(ctx, []byte("canceled"))
		if err == nil {
			t.Error("expected error for canceled context")
		}
	})

	t.Run("GetCanceled", func(t *testing.T) {
		_, err := store.Get(ctx, "any")
		if err == nil {
			t.Error("expected error for canceled context")
		}
	})
}

func createTestAsset(t *testing.T, store *AssetStore, content []byte) string {
	t.Helper()
	id, err := store.Put(context.Background(), content)
	if err != nil {
		t.Fatalf("failed to create test asset: %v", err)
	}
	return id
}

func TestAssetStore_WithFileSystem(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	fs := NewOSFileSystem()
	store := NewAssetStore(tmpDir).WithFileSystem(fs)
	if store.fs != fs {
		t.Error("WithFileSystem failed to set filesystem")
	}
}
