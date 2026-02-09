// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestTaskStore(t *testing.T) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem

	t.Run("Add and List Tasks", func(t *testing.T) {
		store := setupTaskStore(t, fs)
		runAddTaskTest(t, store, ctx)
	})

	t.Run("Update Task", func(t *testing.T) {
		store := setupTaskStore(t, fs)
		runUpdateTaskTest(t, store, ctx)
	})

	t.Run("Update Non-existent Task", func(t *testing.T) {
		store := setupTaskStore(t, fs)
		runUpdateNonExistentTaskTest(t, store, ctx)
	})

	t.Run("Delete Task", func(t *testing.T) {
		store := setupTaskStore(t, fs)
		runDeleteTaskTest(t, store, ctx)
	})

	t.Run("Persistence", func(t *testing.T) {
		runPersistenceTest(t, fs, ctx)
	})

	t.Run("Corrupted JSON", func(t *testing.T) {
		runCorruptedJSONTest(t, fs, ctx)
	})
}

func setupTaskStore(t *testing.T, fs storage.FileSystem) *TaskStore {
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	return NewTaskStore(fs, tasksFile)
}

func runAddTaskTest(t *testing.T, store *TaskStore, ctx context.Context) {
	// Add task
	res, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "add",
		"content": "Implement feature X",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Task added with ID 1") {
		t.Errorf("expected ID 1, got %s", res.Text)
	}

	// List tasks
	res, err = store.ManageTasks(ctx, map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Implement feature X") {
		t.Errorf("task list missing new task: %s", res.Text)
	}
}

func runUpdateTaskTest(t *testing.T, store *TaskStore, ctx context.Context) {
	if _, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "add",
		"content": "Initial",
	}); err != nil {
		t.Fatal(err)
	}

	// Update task
	_, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "update",
		"task_id": 1.0,
		"status":  "completed",
		"content": "Updated",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify update
	res, err := store.ManageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "[x] Updated (completed)") {
		t.Errorf("task status not updated correctly: %s", res.Text)
	}
}

func runUpdateNonExistentTaskTest(t *testing.T, store *TaskStore, ctx context.Context) {
	_, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "update",
		"task_id": 999.0,
		"status":  "completed",
	})
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
	if !strings.Contains(err.Error(), "task not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func runDeleteTaskTest(t *testing.T, store *TaskStore, ctx context.Context) {
	if _, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "add",
		"content": "To be deleted",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "delete",
		"task_id": 1.0,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := store.ManageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "No tasks found") {
		t.Errorf("expected no tasks, got: %s", res.Text)
	}
}

func runPersistenceTest(t *testing.T, fs storage.FileSystem, ctx context.Context) {
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	store1 := NewTaskStore(fs, tasksFile)

	if _, err := store1.ManageTasks(ctx, map[string]interface{}{
		"action":  "add",
		"content": "Persist me",
	}); err != nil {
		t.Fatal(err)
	}

	store2 := NewTaskStore(fs, tasksFile)
	err := store2.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}

	res, err := store2.ManageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Persist me") {
		t.Error("tasks were not persisted")
	}
}

func runCorruptedJSONTest(t *testing.T, fs storage.FileSystem, ctx context.Context) {
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	if err := fs.WriteFile(ctx, tasksFile, []byte("invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewTaskStore(fs, tasksFile)
	err := store.Load(ctx)
	if err == nil {
		t.Fatal("expected error for corrupted JSON")
	}
}

func TestTaskStore_Concurrency(t *testing.T) {
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "stress.json")
	store := NewTaskStore(storage.DefaultFileSystem, tasksFile)
	ctx := context.Background()

	const workers = 100
	runConcurrencyWorkers(t, store, ctx, workers)
	verifyConcurrencyResults(t, store, ctx, tasksFile, workers)
}

func runConcurrencyWorkers(t *testing.T, store *TaskStore, ctx context.Context, workers int) {
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(val int) {
			defer wg.Done()
			executeWorkerTask(t, store, ctx, val)
		}(i)
	}
	wg.Wait()
}

func executeWorkerTask(t *testing.T, store *TaskStore, ctx context.Context, val int) {
	// 1. Add
	res, err := store.ManageTasks(ctx, map[string]interface{}{
		"action":  "add",
		"content": fmt.Sprintf("Task %d", val),
	})
	if err != nil {
		t.Errorf("Add error (worker %d): %v", val, err)
		return
	}

	// Extract ID from "Task added with ID X"
	var taskID float64
	_, err = fmt.Sscanf(res.Text, "Task added with ID %f", &taskID)
	if err != nil {
		t.Errorf("Failed to parse task ID from %q (worker %d): %v", res.Text, val, err)
		return
	}

	// 2. Update
	_, err = store.ManageTasks(ctx, map[string]interface{}{
		"action":  "update",
		"task_id": taskID,
		"status":  "completed",
	})
	if err != nil {
		t.Errorf("Update error (worker %d, task %.0f): %v", val, taskID, err)
	}

	// 3. List
	_, err = store.ManageTasks(ctx, map[string]interface{}{
		"action": "list",
	})
	if err != nil {
		t.Errorf("List error (worker %d): %v", val, err)
	}
}

func verifyConcurrencyResults(t *testing.T, store *TaskStore, ctx context.Context, tasksFile string, workers int) {
	// Final verification
	res, err := store.ManageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("Final list error: %v", err)
	}

	// Count tasks in the list result
	lines := strings.Split(strings.TrimSpace(res.Text), "\n")
	taskCount := 0
	if len(lines) > 1 {
		taskCount = len(lines) - 1
	}

	if taskCount != workers {
		t.Errorf("Expected %d tasks, got %d. Result:\n%s", workers, taskCount, res.Text)
	}

	// Verify disk file
	data, err := storage.DefaultFileSystem.ReadFile(ctx, tasksFile)
	if err != nil {
		t.Fatalf("Failed to read tasks file: %v", err)
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("File contains invalid JSON: %v", err)
	}
	if len(tasks) != workers {
		t.Errorf("File contains %d tasks, expected %d", len(tasks), workers)
	}
}
