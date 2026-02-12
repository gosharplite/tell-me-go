// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"sync"
	"testing"
)

func TestMemoryKVStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("SetAndGet", func(t *testing.T) {
		t.Parallel()
		runKVSetAndGet(t, ctx)
	})
	t.Run("GetAll", func(t *testing.T) {
		t.Parallel()
		runKVGetAll(t, ctx)
	})
	t.Run("Delete", func(t *testing.T) {
		t.Parallel()
		runKVDelete(t, ctx)
	})
	t.Run("NonExistentKey", func(t *testing.T) {
		t.Parallel()
		runKVNonExistentKey(t, ctx)
	})
}

func runKVSetAndGet(t *testing.T, ctx context.Context) {
	store := newMemoryKVStore()
	key, val := "key1", "val1"

	if err := store.Set(ctx, key, val); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != val {
		t.Errorf("Expected %s, got %s", val, got)
	}
}

func runKVGetAll(t *testing.T, ctx context.Context) {
	store := newMemoryKVStore()
	key1, val1 := "key1", "val1"
	key2, val2 := "key2", "val2"

	_ = store.Set(ctx, key1, val1)
	_ = store.Set(ctx, key2, val2)

	all, err := store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 items, got %d", len(all))
	}
	if all[key1] != val1 || all[key2] != val2 {
		t.Errorf("GetAll returned incorrect data: %v", all)
	}
}

func runKVDelete(t *testing.T, ctx context.Context) {
	store := newMemoryKVStore()
	key, val := "key1", "val1"
	_ = store.Set(ctx, key, val)

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	got, _ := store.Get(ctx, key)
	if got != "" {
		t.Errorf("Expected empty string after delete, got %s", got)
	}

	all, _ := store.GetAll(ctx)
	if len(all) != 0 {
		t.Errorf("Expected 0 items after delete, got %d", len(all))
	}
}

func runKVNonExistentKey(t *testing.T, ctx context.Context) {
	store := newMemoryKVStore()
	got, err := store.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != "" {
		t.Errorf("Expected empty string for non-existent key, got %s", got)
	}
}

func TestMemoryListStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("AppendAndReadAll", func(t *testing.T) {
		t.Parallel()
		runListAppendAndReadAll(t, ctx)
	})
	t.Run("WriteAllOverwrites", func(t *testing.T) {
		t.Parallel()
		runListWriteAllOverwrites(t, ctx)
	})
	t.Run("Concurrency", func(t *testing.T) {
		t.Parallel()
		runListConcurrency(t, ctx)
	})
}

func runListAppendAndReadAll(t *testing.T, ctx context.Context) {
	store := newMemoryListStore[string]()
	val1 := "item1"

	if err := store.Append(ctx, val1); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	items, err := store.ReadAll(ctx)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if len(items) != 1 || items[0] != val1 {
		t.Errorf("ReadAll returned incorrect data: %v", items)
	}
}

func runListWriteAllOverwrites(t *testing.T, ctx context.Context) {
	store := newMemoryListStore[string]()
	_ = store.Append(ctx, "old")

	newItems := []string{"new1", "new2"}
	if err := store.WriteAll(ctx, newItems); err != nil {
		t.Fatalf("WriteAll failed: %v", err)
	}

	items, _ := store.ReadAll(ctx)
	if len(items) != 2 || items[0] != "new1" || items[1] != "new2" {
		t.Errorf("WriteAll did not overwrite correctly: %v", items)
	}
}

func runListConcurrency(t *testing.T, ctx context.Context) {
	store := newMemoryListStore[int]()
	var wg sync.WaitGroup
	count := 100

	// Concurrent appends
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			_ = store.Append(ctx, val)
		}(i)
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.WriteAll(ctx, []int{1, 2, 3})
		}()
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.ReadAll(ctx)
		}()
	}

	wg.Wait()

	// Final read to ensure no panic or race
	_, _ = store.ReadAll(ctx)
}
