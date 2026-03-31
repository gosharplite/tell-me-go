package persistence

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	_ "modernc.org/sqlite"
)

func setupSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	if err := createTables(context.Background(), db); err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func setupTestDB(t *testing.T) ports.KVStore {
	t.Helper()
	return newSQLiteKVStore(setupSQLite(t))
}

func TestSQLiteTaskStore(t *testing.T) {
	t.Parallel()
	t.Run("Read Empty Store", testTaskStoreReadEmpty)
	t.Run("Append and Read Tasks", testTaskStoreAppendAndRead)
	t.Run("Update Task", testTaskStoreUpdate)
	t.Run("Delete Task", testTaskStoreDelete)
	t.Run("Delete All Tasks", testTaskStoreDeleteAll)
}

func testTaskStoreReadEmpty(t *testing.T) {
	t.Parallel()
	db := setupSQLite(t)
	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	tasks, err := store.ReadAll(ctx)
	if err != nil {
		t.Errorf("Expected nil error on ReadAll when empty, got %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks, got %d", len(tasks))
	}
}

func testTaskStoreAppendAndRead(t *testing.T) {
	t.Parallel()
	db := setupSQLite(t)
	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	task1 := ports.Task{ID: 1, Content: "task 1", Status: "pending", CreatedAt: now}
	task2 := ports.Task{ID: 2, Content: "task 2", Status: "completed", CreatedAt: now.Add(time.Hour)}

	if err := store.Append(ctx, task1); err != nil {
		t.Errorf("Failed to append task1: %v", err)
	}
	if err := store.Append(ctx, task2); err != nil {
		t.Errorf("Failed to append task2: %v", err)
	}

	tasks, err := store.ReadAll(ctx)
	if err != nil {
		t.Errorf("Failed to read all tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Content != "task 1" || tasks[1].Content != "task 2" {
		t.Errorf("Tasks mismatched: %v", tasks)
	}
	if !tasks[0].CreatedAt.Equal(task1.CreatedAt) {
		t.Errorf("CreatedAt mismatched: expected %v, got %v", task1.CreatedAt, tasks[0].CreatedAt)
	}
}

func testTaskStoreUpdate(t *testing.T) {
	t.Parallel()
	db := setupSQLite(t)
	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	task1 := ports.Task{ID: 1, Content: "task 1", Status: "pending", CreatedAt: now}
	_ = store.Append(ctx, task1) // initial

	task1Updated := task1
	task1Updated.Content = "task 1 updated"
	task1Updated.Status = "in_progress"
	if err := store.Update(ctx, 1, task1Updated); err != nil {
		t.Errorf("Failed to update task 1: %v", err)
	}

	tasks, _ := store.ReadAll(ctx)
	if len(tasks) > 0 && (tasks[0].Content != "task 1 updated" || tasks[0].Status != "in_progress") {
		t.Errorf("Task 1 not updated correctly: %v", tasks[0])
	}
}

func testTaskStoreDelete(t *testing.T) {
	t.Parallel()
	db := setupSQLite(t)
	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	task1 := ports.Task{ID: 1, Content: "task 1", Status: "pending", CreatedAt: now}
	task2 := ports.Task{ID: 2, Content: "task 2", Status: "completed", CreatedAt: now.Add(time.Hour)}
	_ = store.Append(ctx, task1)
	_ = store.Append(ctx, task2)

	if err := store.Delete(ctx, 1); err != nil {
		t.Errorf("Failed to delete task 1: %v", err)
	}
	tasks, _ := store.ReadAll(ctx)
	if len(tasks) != 1 || tasks[0].ID != 2 {
		t.Errorf("Delete failed, remaining tasks: %v", tasks)
	}
}

func testTaskStoreDeleteAll(t *testing.T) {
	t.Parallel()
	db := setupSQLite(t)
	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	task1 := ports.Task{ID: 1, Content: "task 1", Status: "pending", CreatedAt: now}
	_ = store.Append(ctx, task1)

	if err := store.DeleteAll(ctx); err != nil {
		t.Errorf("Failed to delete all tasks: %v", err)
	}
	tasks, _ := store.ReadAll(ctx)
	if len(tasks) != 0 {
		t.Errorf("DeleteAll failed, remaining tasks: %d", len(tasks))
	}
}

func TestStoreErrors(t *testing.T) {
	t.Parallel()

	t.Run("TaskStore Errors", func(t *testing.T) {
		t.Parallel()
		db := setupSQLite(t)
		_ = db.Close() // Simulate connection drop
		store := newSQLiteTaskStore(db)
		ctx := context.Background()

		if _, err := store.ReadAll(ctx); err == nil {
			t.Errorf("Expected error on ReadAll with closed DB")
		}

		if err := store.Append(ctx, ports.Task{ID: 1}); err == nil {
			t.Errorf("Expected error on Append with closed DB")
		}

		if err := store.Update(ctx, 1, ports.Task{ID: 1}); err == nil {
			t.Errorf("Expected error on Update with closed DB")
		}

		if err := store.Delete(ctx, 1); err == nil {
			t.Errorf("Expected error on Delete with closed DB")
		}

		if err := store.DeleteAll(ctx); err == nil {
			t.Errorf("Expected error on DeleteAll with closed DB")
		}
	})
}

func TestSQLiteKVStore(t *testing.T) {
	t.Parallel()

	t.Run("SetAndGet", testKVStoreSetAndGet)
	t.Run("UpdateExisting", testKVStoreUpdateExisting)
	t.Run("Delete", testKVStoreDelete)
	t.Run("GetAll", testKVStoreGetAll)
	t.Run("ContextCancellation", testKVStoreContextCancellation)
	t.Run("DatabaseError", testKVStoreDatabaseError)
}

func testKVStoreSetAndGet(t *testing.T) {
	t.Parallel()
	kv := setupTestDB(t)
	ctx := context.Background()

	if err := kv.Set(ctx, "theme", "dark"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := kv.Get(ctx, "theme")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "dark" {
		t.Errorf("got %q; want %q", val, "dark")
	}

	// Missing key
	val, err = kv.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get missing key failed: %v", err)
	}
	if val != "" {
		t.Errorf("got %q for missing key; want empty", val)
	}
}

func testKVStoreUpdateExisting(t *testing.T) {
	t.Parallel()
	kv := setupTestDB(t)
	ctx := context.Background()

	_ = kv.Set(ctx, "theme", "light")
	if err := kv.Set(ctx, "theme", "dark"); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	val, _ := kv.Get(ctx, "theme")
	if val != "dark" {
		t.Errorf("got %q; want %q", val, "dark")
	}
}

func testKVStoreDelete(t *testing.T) {
	t.Parallel()
	kv := setupTestDB(t)
	ctx := context.Background()

	_ = kv.Set(ctx, "theme", "dark")
	if err := kv.Delete(ctx, "theme"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	val, _ := kv.Get(ctx, "theme")
	if val != "" {
		t.Errorf("got %q after delete; want empty", val)
	}

	// Non-existent key
	if err := kv.Delete(ctx, "non-existent"); err != nil {
		t.Errorf("Delete non-existent key failed: %v", err)
	}
}

func testKVStoreGetAll(t *testing.T) {
	t.Parallel()
	kv := setupTestDB(t)
	ctx := context.Background()

	data := map[string]string{
		"key1": "val1",
		"key2": "val2",
		"key3": "val3",
	}

	for k, v := range data {
		_ = kv.Set(ctx, k, v)
	}

	all, err := kv.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}

	if len(all) != len(data) {
		t.Fatalf("got %d keys; want %d", len(all), len(data))
	}

	for k, v := range data {
		if all[k] != v {
			t.Errorf("key %s: got %q; want %q", k, all[k], v)
		}
	}
}

func testKVStoreContextCancellation(t *testing.T) {
	t.Parallel()
	kv := setupTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := kv.Set(ctx, "theme", "dark"); err == nil {
		t.Error("expected error for cancelled context on Set")
	}

	if _, err := kv.Get(ctx, "theme"); err == nil {
		t.Error("expected error for cancelled context on Get")
	}
}

func testKVStoreDatabaseError(t *testing.T) {
	t.Parallel()
	db := setupSQLite(t)
	kv := newSQLiteKVStore(db)
	_ = db.Close()
	ctx := context.Background()

	if _, err := kv.Get(ctx, "any"); err == nil {
		t.Error("expected error on Get with closed DB")
	}
	if err := kv.Set(ctx, "any", "val"); err == nil {
		t.Error("expected error on Set with closed DB")
	}
	if err := kv.Delete(ctx, "any"); err == nil {
		t.Error("expected error on Delete with closed DB")
	}
	if _, err := kv.GetAll(ctx); err == nil {
		t.Error("expected error on GetAll with closed DB")
	}
}
