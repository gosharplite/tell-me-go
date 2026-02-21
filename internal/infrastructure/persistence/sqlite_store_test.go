package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
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
		db.Close()
	})

	return db
}

func TestSQLiteConfigStore(t *testing.T) {
	db := setupTestDB(t)
	store := newSQLiteConfigStore(db)
	ctx := context.Background()

	// Test Get on empty db
	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Errorf("Expected nil error for nonexistent key, got %v", err)
	}
	if val != "" {
		t.Errorf("Expected empty string for nonexistent key, got %q", val)
	}

	// Test Set
	if err := store.Set(ctx, "key1", "value1"); err != nil {
		t.Errorf("Failed to set key1: %v", err)
	}

	// Test Get existing
	val, err = store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Failed to get key1: %v", err)
	}
	if val != "value1" {
		t.Errorf("Expected 'value1', got %q", val)
	}

	// Test Set update
	if err := store.Set(ctx, "key1", "value2"); err != nil {
		t.Errorf("Failed to update key1: %v", err)
	}
	val, err = store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Failed to get key1 after update: %v", err)
	}
	if val != "value2" {
		t.Errorf("Expected 'value2' after update, got %q", val)
	}

	// Test GetAll
	if err := store.Set(ctx, "key2", "value3"); err != nil {
		t.Errorf("Failed to set key2: %v", err)
	}

	all, err := store.GetAll(ctx)
	if err != nil {
		t.Errorf("Failed to get all configs: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Expected 2 configs, got %d", len(all))
	}
	if all["key1"] != "value2" || all["key2"] != "value3" {
		t.Errorf("GetAll returned unexpected map: %v", all)
	}

	// Test Delete
	if err := store.Delete(ctx, "key1"); err != nil {
		t.Errorf("Failed to delete key1: %v", err)
	}
	val, err = store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Expected nil error after delete, got %v", err)
	}
	if val != "" {
		t.Errorf("Expected empty string after delete, got %q", val)
	}
}

func TestSQLiteScratchpadStore(t *testing.T) {
	db := setupTestDB(t)
	store := newSQLiteScratchpadStore(db)
	ctx := context.Background()

	// Test Get with wrong key
	val, err := store.Get(ctx, "wrong")
	if err != nil {
		t.Errorf("Expected nil error for wrong key, got %v", err)
	}
	if val != "" {
		t.Errorf("Expected empty string for wrong key, got %q", val)
	}

	// Test Get when empty
	val, err = store.Get(ctx, "content")
	if err != nil {
		t.Errorf("Expected nil error for empty content, got %v", err)
	}
	if val != "" {
		t.Errorf("Expected empty string for empty content, got %q", val)
	}

	// Test Set with wrong key
	if err := store.Set(ctx, "wrong", "ignore this"); err != nil {
		t.Errorf("Expected nil error for setting wrong key, got %v", err)
	}
	val, _ = store.Get(ctx, "content")
	if val != "" {
		t.Errorf("Setting wrong key should not affect content, got %q", val)
	}

	// Test Set
	if err := store.Set(ctx, "content", "initial content"); err != nil {
		t.Errorf("Failed to set content: %v", err)
	}
	val, err = store.Get(ctx, "content")
	if err != nil {
		t.Errorf("Failed to get content: %v", err)
	}
	if val != "initial content" {
		t.Errorf("Expected 'initial content', got %q", val)
	}

	// Test Set update
	if err := store.Set(ctx, "content", "updated content"); err != nil {
		t.Errorf("Failed to update content: %v", err)
	}
	val, err = store.Get(ctx, "content")
	if err != nil {
		t.Errorf("Failed to get content after update: %v", err)
	}
	if val != "updated content" {
		t.Errorf("Expected 'updated content', got %q", val)
	}

	// Test GetAll
	all, err := store.GetAll(ctx)
	if err != nil {
		t.Errorf("Failed to get all scratchpad: %v", err)
	}
	if len(all) != 1 || all["content"] != "updated content" {
		t.Errorf("GetAll returned unexpected map: %v", all)
	}

	// Test Delete with wrong key
	if err := store.Delete(ctx, "wrong"); err != nil {
		t.Errorf("Expected nil error for deleting wrong key, got %v", err)
	}
	val, _ = store.Get(ctx, "content")
	if val != "updated content" {
		t.Errorf("Deleting wrong key should not affect content, got %q", val)
	}

	// Test Delete
	if err := store.Delete(ctx, "content"); err != nil {
		t.Errorf("Failed to delete content: %v", err)
	}
	val, err = store.Get(ctx, "content")
	if err != nil {
		t.Errorf("Failed to get content after delete: %v", err)
	}
	if val != "" {
		t.Errorf("Expected empty string after delete, got %q", val)
	}
}

func TestSQLiteTaskStore(t *testing.T) {
	db := setupTestDB(t)
	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	// Test ReadAll when empty
	tasks, err := store.ReadAll(ctx)
	if err != nil {
		t.Errorf("Expected nil error on ReadAll when empty, got %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("Expected 0 tasks, got %d", len(tasks))
	}

	now := time.Now().Truncate(time.Millisecond)

	task1 := services.Task{
		ID:        1,
		Content:   "task 1",
		Status:    "pending",
		CreatedAt: now,
	}
	task2 := services.Task{
		ID:        2,
		Content:   "task 2",
		Status:    "completed",
		CreatedAt: now.Add(time.Hour),
	}

	// Test Append
	if err := store.Append(ctx, task1); err != nil {
		t.Errorf("Failed to append task1: %v", err)
	}
	if err := store.Append(ctx, task2); err != nil {
		t.Errorf("Failed to append task2: %v", err)
	}

	// Test ReadAll
	tasks, err = store.ReadAll(ctx)
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

	// Test Update
	task1Updated := task1
	task1Updated.Content = "task 1 updated"
	task1Updated.Status = "in_progress"
	if err := store.Update(ctx, 1, task1Updated); err != nil {
		t.Errorf("Failed to update task 1: %v", err)
	}

	tasks, _ = store.ReadAll(ctx)
	if len(tasks) > 0 && (tasks[0].Content != "task 1 updated" || tasks[0].Status != "in_progress") {
		t.Errorf("Task 1 not updated correctly: %v", tasks[0])
	}

	// Test Delete
	if err := store.Delete(ctx, 1); err != nil {
		t.Errorf("Failed to delete task 1: %v", err)
	}
	tasks, _ = store.ReadAll(ctx)
	if len(tasks) != 1 || tasks[0].ID != 2 {
		t.Errorf("Delete failed, remaining tasks: %v", tasks)
	}

	// Test DeleteAll
	if err := store.DeleteAll(ctx); err != nil {
		t.Errorf("Failed to delete all tasks: %v", err)
	}
	tasks, _ = store.ReadAll(ctx)
	if len(tasks) != 0 {
		t.Errorf("DeleteAll failed, remaining tasks: %d", len(tasks))
	}
}

func TestStoreErrors(t *testing.T) {
	db := setupTestDB(t)
	db.Close() // Close DB to force errors

	ctx := context.Background()

	// Config Store Errors
	configStore := newSQLiteConfigStore(db)
	if _, err := configStore.Get(ctx, "key"); err == nil {
		t.Errorf("Expected error on closed db Get")
	}
	if _, err := configStore.GetAll(ctx); err == nil {
		t.Errorf("Expected error on closed db GetAll")
	}

	// Scratchpad Store Errors
	scratchStore := newSQLiteScratchpadStore(db)
	if _, err := scratchStore.Get(ctx, "content"); err == nil {
		t.Errorf("Expected error on closed db Get")
	}
	if _, err := scratchStore.GetAll(ctx); err == nil {
		t.Errorf("Expected error on closed db GetAll")
	}

	// Task Store Errors
	taskStore := newSQLiteTaskStore(db)
	if _, err := taskStore.ReadAll(ctx); err == nil {
		t.Errorf("Expected error on closed db ReadAll")
	}
}

func TestSQLiteTaskStore_ReadPage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := newSQLiteTaskStore(db)
	ctx := context.Background()

	// Insert 5 tasks
	for i := 1; i <= 5; i++ {
		task := services.Task{
			ID:        float64(i),
			Content:   fmt.Sprintf("task %d", i),
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		err := store.Append(ctx, task)
		if err != nil {
			t.Fatalf("failed to append task %d: %v", i, err)
		}
	}

	// Read page 1: limit 2, offset 0 -> IDs 1, 2
	page1, err := store.ReadPage(ctx, 2, 0)
	if err != nil {
		t.Fatalf("failed to read page 1: %v", err)
	}
	if len(page1) != 2 || page1[0].ID != 1 || page1[1].ID != 2 {
		t.Errorf("unexpected page 1: %+v", page1)
	}

	// Read page 2: limit 2, offset 2 -> IDs 3, 4
	page2, err := store.ReadPage(ctx, 2, 2)
	if err != nil {
		t.Fatalf("failed to read page 2: %v", err)
	}
	if len(page2) != 2 || page2[0].ID != 3 || page2[1].ID != 4 {
		t.Errorf("unexpected page 2: %+v", page2)
	}

	// Read page 3: limit 2, offset 4 -> ID 5
	page3, err := store.ReadPage(ctx, 2, 4)
	if err != nil {
		t.Fatalf("failed to read page 3: %v", err)
	}
	if len(page3) != 1 || page3[0].ID != 5 {
		t.Errorf("unexpected page 3: %+v", page3)
	}

	// Read page out of bounds: limit 2, offset 6 -> empty
	page4, err := store.ReadPage(ctx, 2, 6)
	if err != nil {
		t.Fatalf("failed to read page 4: %v", err)
	}
	if len(page4) != 0 {
		t.Errorf("unexpected page 4: %+v", page4)
	}
}
