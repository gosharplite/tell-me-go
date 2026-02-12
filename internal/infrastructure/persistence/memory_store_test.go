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
	store := NewMemoryKVStore()

	// Test Set and Get
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

	// Test GetAll
	key2, val2 := "key2", "val2"
	_ = store.Set(ctx, key2, val2)

	all, err := store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 items, got %d", len(all))
	}
	if all[key] != val || all[key2] != val2 {
		t.Errorf("GetAll returned incorrect data: %v", all)
	}

	// Test Delete
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	got, _ = store.Get(ctx, key)
	if got != "" {
		t.Errorf("Expected empty string after delete, got %s", got)
	}

	all, _ = store.GetAll(ctx)
	if len(all) != 1 {
		t.Errorf("Expected 1 item after delete, got %d", len(all))
	}
}

func TestMemoryListStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("BasicCRUD", func(t *testing.T) {
		store := NewMemoryListStore[string]()

		// Test Append and ReadAll
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

		// Test WriteAll
		newItems := []string{"new1", "new2"}
		if err := store.WriteAll(ctx, newItems); err != nil {
			t.Fatalf("WriteAll failed: %v", err)
		}

		items, _ = store.ReadAll(ctx)
		if len(items) != 2 || items[0] != "new1" || items[1] != "new2" {
			t.Errorf("WriteAll did not overwrite correctly: %v", items)
		}
	})

	t.Run("Concurrency", func(t *testing.T) {
		store := NewMemoryListStore[int]()
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

		// Concurrent reads
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = store.ReadAll(ctx)
			}()
		}

		wg.Wait()

		items, _ := store.ReadAll(ctx)
		if len(items) != count {
			t.Errorf("Expected %d items, got %d", count, len(items))
		}
	})
}
