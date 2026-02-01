// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"path/filepath"
	"strings"
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
