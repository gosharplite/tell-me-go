package persistence

import (
	"context"
	"database/sql"
	"fmt"
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
}

func TestSQLiteKVStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		action  func(ctx context.Context, store ports.KVStore) (string, error)
		want    string
		wantErr bool
	}{
		{
			name: "Set and Get",
			action: func(ctx context.Context, store ports.KVStore) (string, error) {
				if err := store.Set(ctx, "theme", "dark"); err != nil {
					return "", err
				}
				return store.Get(ctx, "theme")
			},
			want: "dark",
		},
		{
			name: "Missing Key",
			action: func(ctx context.Context, store ports.KVStore) (string, error) {
				return store.Get(ctx, "non_existent")
			},
			want: "",
		},
		{
			name: "Overwrite (Upsert)",
			action: func(ctx context.Context, store ports.KVStore) (string, error) {
				if err := store.Set(ctx, "theme", "light"); err != nil {
					return "", err
				}
				if err := store.Set(ctx, "theme", "dark"); err != nil {
					return "", err
				}
				return store.Get(ctx, "theme")
			},
			want: "dark",
		},
		{
			name: "Delete",
			action: func(ctx context.Context, store ports.KVStore) (string, error) {
				if err := store.Set(ctx, "theme", "dark"); err != nil {
					return "", err
				}
				if err := store.Delete(ctx, "theme"); err != nil {
					return "", err
				}
				return store.Get(ctx, "theme")
			},
			want: "",
		},
		{
			name: "GetAll",
			action: func(ctx context.Context, store ports.KVStore) (string, error) {
				_ = store.Set(ctx, "key1", "val1")
				_ = store.Set(ctx, "key2", "val2")
				_ = store.Set(ctx, "key3", "val3")
				all, err := store.GetAll(ctx)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%d", len(all)), nil
			},
			want: "3",
		},
		{
			name: "Closed Database Error",
			action: func(ctx context.Context, store ports.KVStore) (string, error) {
				// Use type assertion to close the underlying DB
				if s, ok := store.(*sqliteKVStore); ok {
					_ = s.db.Close()
				}
				return store.Get(ctx, "any")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable for safe parallel execution
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 1. ISOLATION: Setup DB INSIDE the loop to guarantee a clean environment
			db := setupTestDB(t)
			store := newSQLiteKVStore(db)

			// 2. EXECUTION
			got, err := tt.action(context.Background(), store)

			// 3. ASSERTION
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q; want %q", got, tt.want)
			}
		})
	}
}
