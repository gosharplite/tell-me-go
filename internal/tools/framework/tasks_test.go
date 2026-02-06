// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

func TestTaskStore(t *testing.T) {
	ctx := context.Background()
	fs := fsutil.DefaultFileSystem

	t.Run("Add and List Tasks", func(t *testing.T) {
		tempDir := t.TempDir()
		tasksFile := filepath.Join(tempDir, "tasks.json")
		store := NewTaskStore(fs, tasksFile)

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
	})

	t.Run("Update Task", func(t *testing.T) {
		tempDir := t.TempDir()
		tasksFile := filepath.Join(tempDir, "tasks.json")
		store := NewTaskStore(fs, tasksFile)

		store.ManageTasks(ctx, map[string]interface{}{
			"action":  "add",
			"content": "Initial",
		})

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
		res, _ := store.ManageTasks(ctx, map[string]interface{}{"action": "list"})
		if !strings.Contains(res.Text, "[x] Updated (completed)") {
			t.Errorf("task status not updated correctly: %s", res.Text)
		}
	})

	t.Run("Update Non-existent Task", func(t *testing.T) {
		tempDir := t.TempDir()
		tasksFile := filepath.Join(tempDir, "tasks.json")
		store := NewTaskStore(fs, tasksFile)

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
	})

	t.Run("Delete Task", func(t *testing.T) {
		tempDir := t.TempDir()
		tasksFile := filepath.Join(tempDir, "tasks.json")
		store := NewTaskStore(fs, tasksFile)

		store.ManageTasks(ctx, map[string]interface{}{
			"action":  "add",
			"content": "To be deleted",
		})

		_, err := store.ManageTasks(ctx, map[string]interface{}{
			"action":  "delete",
			"task_id": 1.0,
		})
		if err != nil {
			t.Fatal(err)
		}

		res, _ := store.ManageTasks(ctx, map[string]interface{}{"action": "list"})
		if !strings.Contains(res.Text, "No tasks found") {
			t.Errorf("expected no tasks, got: %s", res.Text)
		}
	})

	t.Run("Persistence", func(t *testing.T) {
		tempDir := t.TempDir()
		tasksFile := filepath.Join(tempDir, "tasks.json")
		store1 := NewTaskStore(fs, tasksFile)

		store1.ManageTasks(ctx, map[string]interface{}{
			"action":  "add",
			"content": "Persist me",
		})

		store2 := NewTaskStore(fs, tasksFile)
		err := store2.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}

		res, _ := store2.ManageTasks(ctx, map[string]interface{}{"action": "list"})
		if !strings.Contains(res.Text, "Persist me") {
			t.Error("tasks were not persisted")
		}
	})

	t.Run("Corrupted JSON", func(t *testing.T) {
		tempDir := t.TempDir()
		tasksFile := filepath.Join(tempDir, "tasks.json")
		fs.WriteFile(ctx, tasksFile, []byte("invalid json"), 0644)

		store := NewTaskStore(fs, tasksFile)
		err := store.Load(ctx)
		if err == nil {
			t.Fatal("expected error for corrupted JSON")
		}
	})
}

func TestTaskStore_Concurrency(t *testing.T) {
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "stress.json")
	store := NewTaskStore(fsutil.DefaultFileSystem, tasksFile)
	ctx := context.Background()

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(val int) {
			defer wg.Done()

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
		}(i)
	}
	wg.Wait()

	// Final verification
	res, err := store.ManageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("Final list error: %v", err)
	}

	// Count tasks in the list result
	lines := strings.Split(strings.TrimSpace(res.Text), "\n")
	// Header is "Tasks:", so tasks start from index 1
	taskCount := 0
	if len(lines) > 1 {
		taskCount = len(lines) - 1
	}

	if taskCount != workers {
		t.Errorf("Expected %d tasks, got %d. Result:\n%s", workers, taskCount, res.Text)
	}

	// Verify disk file
	data, err := fsutil.DefaultFileSystem.ReadFile(ctx, tasksFile)
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
