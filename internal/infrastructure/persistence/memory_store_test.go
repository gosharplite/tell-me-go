// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
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
	t.Run("UpdateAndDelete", func(t *testing.T) {
		t.Parallel()
		runListUpdateAndDelete(t, ctx)
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

func runListUpdateAndDelete(t *testing.T, ctx context.Context) {
	store := newMemoryListStore[services.Task]()
	_ = store.Append(ctx, services.Task{ID: 1, Content: "old"})

	if err := store.Update(ctx, 1, services.Task{ID: 1, Content: "new"}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	items, _ := store.ReadAll(ctx)
	if len(items) != 1 || items[0].Content != "new" {
		t.Errorf("Update did not overwrite correctly: %v", items)
	}

	if err := store.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	items, _ = store.ReadAll(ctx)
	if len(items) != 0 {
		t.Errorf("Delete did not remove correctly: %v", items)
	}

	_ = store.Append(ctx, services.Task{ID: 2, Content: "item"})
	if err := store.DeleteAll(ctx); err != nil {
		t.Fatalf("DeleteAll failed: %v", err)
	}
	items, _ = store.ReadAll(ctx)
	if len(items) != 0 {
		t.Errorf("DeleteAll did not clear items")
	}
}

func runListConcurrency(t *testing.T, ctx context.Context) {
	store := newMemoryListStore[services.Task]()
	var wg sync.WaitGroup
	count := 100

	// Concurrent appends
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(val float64) {
			defer wg.Done()
			_ = store.Append(ctx, services.Task{ID: val})
		}(float64(i))
	}

	// Concurrent updates
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(val float64) {
			defer wg.Done()
			_ = store.Update(ctx, val, services.Task{ID: val, Content: "updated"})
		}(float64(i))
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

func TestMemoryListStore_ReadPage(t *testing.T) {
	store := newMemoryListStore[services.Task]()
	ctx := context.Background()

	// Add 5 tasks
	for i := 1; i <= 5; i++ {
		err := store.Append(ctx, services.Task{ID: float64(i), Content: "task"})
		if err != nil {
			t.Fatalf("unexpected error appending task: %v", err)
		}
	}

	tests := []struct {
		name          string
		limit         int
		offset        int
		expectedCount int
		expectedIDs   []float64
	}{
		{"First Page", 2, 0, 2, []float64{1, 2}},
		{"Middle Page", 2, 2, 2, []float64{3, 4}},
		{"Last Page", 2, 4, 1, []float64{5}},
		{"Out of Bounds", 2, 6, 0, []float64{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := store.ReadPage(ctx, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("unexpected error reading page %s: %v", tt.name, err)
			}
			if len(page) != tt.expectedCount {
				t.Fatalf("expected %d items, got %d", tt.expectedCount, len(page))
			}
			for i, expectedID := range tt.expectedIDs {
				if page[i].ID != expectedID {
					t.Errorf("expected ID %v at index %d, got %v", expectedID, i, page[i].ID)
				}
			}
		})
	}
}
