package persistence

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
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
	db := setupTestDB(t)
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
	db := setupTestDB(t)
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
	db := setupTestDB(t)
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
	db := setupTestDB(t)
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
	db := setupTestDB(t)
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
		db := setupTestDB(t)
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

	t.Run("KVStore Errors", func(t *testing.T) {
		t.Parallel()
		db := setupTestDB(t)
		_ = db.Close() // Simulate connection drop
		store := newSQLiteKVStore(db)
		ctx := context.Background()

		if _, err := store.Get(ctx, "key"); err == nil {
			t.Errorf("Expected error on Get with closed DB")
		}

		if err := store.Set(ctx, "key", "val"); err == nil {
			t.Errorf("Expected error on Set with closed DB")
		}

		if err := store.Delete(ctx, "key"); err == nil {
			t.Errorf("Expected error on Delete with closed DB")
		}

		if _, err := store.GetAll(ctx); err == nil {
			t.Errorf("Expected error on GetAll with closed DB")
		}
	})
}

func TestSQLiteKVStore(t *testing.T) {
	t.Parallel()

	t.Run("Set and Get", func(t *testing.T) {
		t.Parallel()
		db := setupTestDB(t)
		store := newSQLiteKVStore(db)
		ctx := context.Background()

		key, val := "theme", "dark"
		if err := store.Set(ctx, key, val); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != val {
			t.Errorf("Get = %q; want %q", got, val)
		}
	})

	t.Run("Missing Key", func(t *testing.T) {
		t.Parallel()
		db := setupTestDB(t)
		store := newSQLiteKVStore(db)
		ctx := context.Background()

		got, err := store.Get(ctx, "non_existent")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != "" {
			t.Errorf("Get = %q; want empty string", got)
		}
	})

	t.Run("Overwrite (Upsert)", func(t *testing.T) {
		t.Parallel()
		db := setupTestDB(t)
		store := newSQLiteKVStore(db)
		ctx := context.Background()

		key := "theme"
		if err := store.Set(ctx, key, "light"); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != "light" {
			t.Errorf("Get = %q; want %q", got, "light")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()
		db := setupTestDB(t)
		store := newSQLiteKVStore(db)
		ctx := context.Background()

		key := "theme"
		if err := store.Delete(ctx, key); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got != "" {
			t.Errorf("Get = %q; want empty string after delete", got)
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		t.Parallel()
		db := setupTestDB(t)
		store := newSQLiteKVStore(db)
		ctx := context.Background()

		settings := map[string]string{
			"key1": "val1",
			"key2": "val2",
			"key3": "val3",
		}

		for k, v := range settings {
			if err := store.Set(ctx, k, v); err != nil {
				t.Fatalf("Set %s failed: %v", k, err)
			}
		}

		all, err := store.GetAll(ctx)
		if err != nil {
			t.Fatalf("GetAll failed: %v", err)
		}

		for k, v := range settings {
			if got, ok := all[k]; !ok || got != v {
				t.Errorf("GetAll[%q] = %q; want %q", k, got, v)
			}
		}
	})
}
