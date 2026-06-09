// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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
	store := newMemoryListStore[ports.Task]()
	_ = store.Append(ctx, ports.Task{ID: 1, Content: "old"})

	if err := store.Update(ctx, 1, ports.Task{ID: 1, Content: "new"}); err != nil {
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

	_ = store.Append(ctx, ports.Task{ID: 2, Content: "item"})
	if err := store.DeleteAll(ctx); err != nil {
		t.Fatalf("DeleteAll failed: %v", err)
	}
	items, _ = store.ReadAll(ctx)
	if len(items) != 0 {
		t.Errorf("DeleteAll did not clear items")
	}
}

func runListConcurrency(t *testing.T, ctx context.Context) {
	store := newMemoryListStore[ports.Task]()
	var wg sync.WaitGroup
	count := 100

	// Concurrent appends
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(val int64) {
			defer wg.Done()
			_ = store.Append(ctx, ports.Task{ID: val})
		}(int64(i))
	}

	// Concurrent updates
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(val int64) {
			defer wg.Done()
			_ = store.Update(ctx, val, ports.Task{ID: val, Content: "updated"})
		}(int64(i))
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

// =============================================================================
// Count — verifies empty, populated, and after-DeleteAll counts
// =============================================================================

func TestMemoryListStore_Count(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		store := newMemoryListStore[ports.Task]()
		n, err := store.Count(ctx)
		if err != nil {
			t.Fatalf("Count failed: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0, got %d", n)
		}
	})

	t.Run("with items", func(t *testing.T) {
		t.Parallel()
		store := newMemoryListStore[ports.Task]()
		_ = store.Append(ctx, ports.Task{ID: 1})
		_ = store.Append(ctx, ports.Task{ID: 2})
		_ = store.Append(ctx, ports.Task{ID: 3})
		n, err := store.Count(ctx)
		if err != nil {
			t.Fatalf("Count failed: %v", err)
		}
		if n != 3 {
			t.Errorf("expected 3, got %d", n)
		}
	})

	t.Run("after delete all", func(t *testing.T) {
		t.Parallel()
		store := newMemoryListStore[ports.Task]()
		_ = store.Append(ctx, ports.Task{ID: 1})
		_ = store.Append(ctx, ports.Task{ID: 2})
		_ = store.DeleteAll(ctx)
		n, err := store.Count(ctx)
		if err != nil {
			t.Fatalf("Count failed: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0, got %d", n)
		}
	})
}

// =============================================================================
// getID edge case — non-struct type falls back to 0
// =============================================================================

func TestMemoryListStore_GetID_NonStruct(t *testing.T) {
	t.Parallel()

	// getID on a non-struct type (string) should return 0
	store := newMemoryListStore[string]()
	id := store.getID("not-a-struct")
	if id != 0 {
		t.Errorf("expected 0 for non-struct type, got %v", id)
	}
}

// =============================================================================
// Update with non-existent ID — no-op, returns nil
// =============================================================================

func TestMemoryListStore_Update_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMemoryListStore[ports.Task]()

	// Append one item, then try to update a non-existent ID
	_ = store.Append(ctx, ports.Task{ID: 1, Content: "existing"})

	err := store.Update(ctx, 999, ports.Task{ID: 999, Content: "not found"})
	if err != nil {
		t.Errorf("Update for non-existent ID should not error, got: %v", err)
	}

	// Verify existing data is unchanged
	items, _ := store.ReadAll(ctx)
	if len(items) != 1 || items[0].Content != "existing" {
		t.Errorf("data was modified by failed update: %v", items)
	}
}

// =============================================================================
// Delete with non-existent ID — no-op, returns nil
// =============================================================================

func TestMemoryListStore_Delete_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMemoryListStore[ports.Task]()

	_ = store.Append(ctx, ports.Task{ID: 1, Content: "existing"})

	err := store.Delete(ctx, 999)
	if err != nil {
		t.Errorf("Delete for non-existent ID should not error, got: %v", err)
	}

	// Verify existing data is unchanged
	items, _ := store.ReadAll(ctx)
	if len(items) != 1 || items[0].Content != "existing" {
		t.Errorf("data was lost during failed delete: %v", items)
	}
}

// =============================================================================
// Query — filter, limit, offset scenarios
// =============================================================================

func TestMemoryListStore_Query(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Seed 6 tasks with mixed statuses and staggered CreatedAt timestamps.
	baseTime := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	store := newMemoryListStore[ports.Task]()
	_ = store.Append(ctx, ports.Task{ID: 1, Content: "task-1", Status: "pending", CreatedAt: baseTime})
	_ = store.Append(ctx, ports.Task{ID: 2, Content: "task-2", Status: "completed", CreatedAt: baseTime.Add(1 * time.Hour)})
	_ = store.Append(ctx, ports.Task{ID: 3, Content: "task-3", Status: "pending", CreatedAt: baseTime.Add(2 * time.Hour)})
	_ = store.Append(ctx, ports.Task{ID: 4, Content: "task-4", Status: "completed", CreatedAt: baseTime.Add(3 * time.Hour)})
	_ = store.Append(ctx, ports.Task{ID: 5, Content: "task-5", Status: "pending", CreatedAt: baseTime.Add(4 * time.Hour)})
	_ = store.Append(ctx, ports.Task{ID: 6, Content: "task-6", Status: "completed", CreatedAt: baseTime.Add(5 * time.Hour)})

	tests := []struct {
		name   string
		filter ports.ListFilter
		limit  int
		offset int
		want   int
	}{
		{
			name:   "all no filter",
			filter: ports.ListFilter{},
			limit:  0,
			offset: 0,
			want:   6,
		},
		{
			name:   "status filter",
			filter: ports.ListFilter{Status: "pending"},
			limit:  0,
			offset: 0,
			want:   3,
		},
		{
			name:   "not_status filter",
			filter: ports.ListFilter{NotStatus: "completed"},
			limit:  0,
			offset: 0,
			want:   3,
		},
		{
			name:   "since filter",
			filter: ports.ListFilter{Since: baseTime.Add(90 * time.Minute)},
			limit:  0,
			offset: 0,
			want:   4,
		},
		{
			name:   "before filter",
			filter: ports.ListFilter{Before: baseTime.Add(150 * time.Minute)},
			limit:  0,
			offset: 0,
			want:   3,
		},
		{
			name:   "limit enforced",
			filter: ports.ListFilter{},
			limit:  2,
			offset: 0,
			want:   2,
		},
		{
			name:   "offset beyond",
			filter: ports.ListFilter{},
			limit:  0,
			offset: 999,
			want:   0,
		},
		{
			name:   "offset + limit",
			filter: ports.ListFilter{},
			limit:  1,
			offset: 1,
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.Query(ctx, tt.filter, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
			if len(result) != tt.want {
				t.Errorf("got %d items, want %d", len(result), tt.want)
			}
		})
	}
}

// =============================================================================
// applyOffsetLimit — slice boundary behaviors
// =============================================================================

func TestMemoryListStore_ApplyOffsetLimit(t *testing.T) {
	t.Parallel()

	input := []int{1, 2, 3, 4, 5}

	tests := []struct {
		name     string
		offset   int
		limit    int
		expected []int
	}{
		{
			name:     "no offset or limit",
			offset:   0,
			limit:    0,
			expected: []int{1, 2, 3, 4, 5},
		},
		{
			name:     "limit only",
			offset:   0,
			limit:    3,
			expected: []int{1, 2, 3},
		},
		{
			name:     "offset only",
			offset:   2,
			limit:    0,
			expected: []int{3, 4, 5},
		},
		{
			name:     "offset + limit",
			offset:   1,
			limit:    2,
			expected: []int{2, 3},
		},
		{
			name:     "offset >= len",
			offset:   5,
			limit:    10,
			expected: []int{},
		},
		{
			name:     "offset exactly at last element",
			offset:   4,
			limit:    0,
			expected: []int{5},
		},
		{
			name:     "limit larger than remaining",
			offset:   3,
			limit:    10,
			expected: []int{4, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyOffsetLimit(input, tt.offset, tt.limit)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("applyOffsetLimit(%v, %d, %d) = %v; want %v",
					input, tt.offset, tt.limit, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// getCreatedAt — struct with valid CreatedAt vs non-struct fallback
// =============================================================================

// =============================================================================
// matchesFilter — individual filter branches
// =============================================================================

func TestMemoryListStore_MatchesFilter(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	store := newMemoryListStore[ports.Task]()

	tests := []struct {
		name     string
		item     ports.Task
		filter   ports.ListFilter
		expected bool
	}{
		{
			name:     "status match",
			item:     ports.Task{Status: "pending", CreatedAt: t0},
			filter:   ports.ListFilter{Status: "pending"},
			expected: true,
		},
		{
			name:     "status mismatch",
			item:     ports.Task{Status: "pending", CreatedAt: t0},
			filter:   ports.ListFilter{Status: "completed"},
			expected: false,
		},
		{
			name:     "not_status match",
			item:     ports.Task{Status: "pending", CreatedAt: t0},
			filter:   ports.ListFilter{NotStatus: "completed"},
			expected: true,
		},
		{
			name:     "not_status excluded",
			item:     ports.Task{Status: "completed", CreatedAt: t0},
			filter:   ports.ListFilter{NotStatus: "completed"},
			expected: false,
		},
		{
			name:     "since — item after",
			item:     ports.Task{Status: "pending", CreatedAt: t0.Add(1 * time.Hour)},
			filter:   ports.ListFilter{Since: t0},
			expected: true,
		},
		{
			name:     "since — item before",
			item:     ports.Task{Status: "pending", CreatedAt: t0.Add(-1 * time.Hour)},
			filter:   ports.ListFilter{Since: t0},
			expected: false,
		},
		{
			name:     "before — item before",
			item:     ports.Task{Status: "pending", CreatedAt: t0.Add(-1 * time.Hour)},
			filter:   ports.ListFilter{Before: t0},
			expected: true,
		},
		{
			name:     "before — item after",
			item:     ports.Task{Status: "pending", CreatedAt: t0.Add(1 * time.Hour)},
			filter:   ports.ListFilter{Before: t0},
			expected: false,
		},
		{
			name:     "empty filter",
			item:     ports.Task{Status: "pending", CreatedAt: t0},
			filter:   ports.ListFilter{},
			expected: true,
		},
		{
			name:     "combined — match all",
			item:     ports.Task{Status: "pending", CreatedAt: t0.Add(1 * time.Hour)},
			filter:   ports.ListFilter{Status: "pending", Since: t0},
			expected: true,
		},
		{
			name:     "combined — one fails",
			item:     ports.Task{Status: "completed", CreatedAt: t0.Add(1 * time.Hour)},
			filter:   ports.ListFilter{Status: "pending", Since: t0},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := store.matchesFilter(tt.item, tt.filter)
			if got != tt.expected {
				t.Errorf("matchesFilter(%+v, %+v) = %v; want %v", tt.item, tt.filter, got, tt.expected)
			}
		})
	}
}

func TestMemoryListStore_GetCreatedAt(t *testing.T) {
	t.Parallel()

	t.Run("struct with valid CreatedAt", func(t *testing.T) {
		t.Parallel()
		store := newMemoryListStore[ports.Task]()
		now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
		got := store.getCreatedAt(ports.Task{CreatedAt: now})
		if !got.Equal(now) {
			t.Errorf("expected %v, got %v", now, got)
		}
	})

	t.Run("non-struct type returns zero time", func(t *testing.T) {
		t.Parallel()
		store := newMemoryListStore[string]()
		got := store.getCreatedAt("not-a-struct")
		if !got.IsZero() {
			t.Errorf("expected zero time for non-struct type, got %v", got)
		}
	})
}

// =============================================================================
// ReadAll completed truncation — verifies that ReadAll returns at most
// (active count) + 500 most recent completed tasks.
// =============================================================================

func TestMemoryListStore_ReadAll_CompletedTruncation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := newMemoryListStore[ports.Task]()

	baseTime := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	// 2 active (pending) tasks
	_ = store.Append(ctx, ports.Task{ID: 1, Content: "task-1", Status: "pending", CreatedAt: baseTime})
	_ = store.Append(ctx, ports.Task{ID: 2, Content: "task-2", Status: "pending", CreatedAt: baseTime.Add(1 * time.Second)})

	// 600 completed tasks — IDs 3 through 602, staggered CreatedAt for deterministic ordering
	for i := 3; i <= 602; i++ {
		_ = store.Append(ctx, ports.Task{
			ID:        int64(i),
			Content:   fmt.Sprintf("task-%d", i),
			Status:    "completed",
			CreatedAt: baseTime.Add(time.Duration(i) * time.Second),
		})
	}

	items, err := store.ReadAll(ctx)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	t.Run("total_count", func(t *testing.T) {
		runReadAllTotalCount(t, items)
	})
	t.Run("active_present", func(t *testing.T) {
		runReadAllActivePresent(t, items)
	})
	t.Run("completed_truncation", func(t *testing.T) {
		runReadAllCompletedTruncation(t, items)
	})
}

func runReadAllTotalCount(t *testing.T, items []ports.Task) {
	t.Helper()
	if len(items) != 502 {
		t.Errorf("expected 502 items (2 active + 500 most recent completed), got %d", len(items))
	}
}

func runReadAllActivePresent(t *testing.T, items []ports.Task) {
	t.Helper()
	if len(items) < 2 {
		t.Fatalf("expected at least 2 items, got %d", len(items))
	}
	if items[0].ID != 1 || items[0].Status != "pending" {
		t.Errorf("first item should be active task ID 1, got ID %d status %s", items[0].ID, items[0].Status)
	}
	if items[1].ID != 2 || items[1].Status != "pending" {
		t.Errorf("second item should be active task ID 2, got ID %d status %s", items[1].ID, items[1].Status)
	}
}

func runReadAllCompletedTruncation(t *testing.T, items []ports.Task) {
	t.Helper()
	// The remaining 500 should be completed tasks IDs 103–602
	completedSlice := items[2:]

	t.Run("slice_length", func(t *testing.T) {
		if len(completedSlice) != 500 {
			t.Fatalf("expected 500 completed items, got %d", len(completedSlice))
		}
	})

	t.Run("slice_boundaries", func(t *testing.T) {
		// Verify first completed is ID 103 (oldest retained)
		if completedSlice[0].ID != 103 {
			t.Errorf("first completed item should be ID 103, got ID %d", completedSlice[0].ID)
		}
		// Verify last completed is ID 602 (newest)
		if completedSlice[499].ID != 602 {
			t.Errorf("last completed item should be ID 602, got ID %d", completedSlice[499].ID)
		}
	})

	t.Run("all_status_completed", func(t *testing.T) {
		// Verify all items in completed slice have status "completed"
		for _, item := range completedSlice {
			if item.Status != "completed" {
				t.Errorf("item ID %d has status %q, want %q", item.ID, item.Status, "completed")
			}
		}
	})

	t.Run("oldest_truncated", func(t *testing.T) {
		// Verify oldest completed tasks (IDs 3–102) are NOT present
		ids := make(map[int64]bool, len(items))
		for _, item := range items {
			ids[item.ID] = true
		}
		for id := int64(3); id <= 102; id++ {
			if ids[id] {
				t.Errorf("oldest completed task ID %d should have been truncated", id)
			}
		}
	})
}
