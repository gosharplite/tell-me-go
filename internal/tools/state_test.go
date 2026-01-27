package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManageTasks(t *testing.T) {
	// Create a temporary directory for homeDir
	tmpDir, err := os.MkdirTemp("", "tools_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Helper to read the tasks file directly
	readTasksFile := func() []Task {
		path := filepath.Join(tmpDir, "output", "global-tasks.json")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return []Task{}
		}
		if err != nil {
			t.Fatalf("Failed to read tasks file: %v", err)
		}
		var tasks []Task
		if len(data) > 0 {
			if err := json.Unmarshal(data, &tasks); err != nil {
				t.Fatalf("Failed to unmarshal tasks: %v", err)
			}
		}
		return tasks
	}

	// Test 1: Add a task
	t.Run("Add", func(t *testing.T) {
		args := map[string]interface{}{
			"action":  "add",
			"content": "First Task",
		}
		msg, err := manageTasks(args, tmpDir)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "Task added with ID: 1") {
			t.Errorf("Unexpected output: %s", msg)
		}

		tasks := readTasksFile()
		if len(tasks) != 1 {
			t.Errorf("Expected 1 task, got %d", len(tasks))
		}
		if tasks[0].Content != "First Task" || tasks[0].Status != "pending" {
			t.Errorf("Task data mismatch: %+v", tasks[0])
		}
	})

	// Test 2: List tasks
	t.Run("List", func(t *testing.T) {
		args := map[string]interface{}{
			"action": "list",
		}
		msg, err := manageTasks(args, tmpDir)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "[1] [pending] First Task") {
			t.Errorf("List output missing task: %s", msg)
		}
	})

	// Test 3: Update task
	t.Run("Update", func(t *testing.T) {
		args := map[string]interface{}{
			"action":  "update",
			"task_id": 1.0, // JSON numbers are floats
			"status":  "completed",
		}
		msg, err := manageTasks(args, tmpDir)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "Task 1 updated") {
			t.Errorf("Unexpected output: %s", msg)
		}

		tasks := readTasksFile()
		if tasks[0].Status != "completed" {
			t.Errorf("Task status not updated: %s", tasks[0].Status)
		}
	})

	// Test 4: Delete task
	t.Run("Delete", func(t *testing.T) {
		args := map[string]interface{}{
			"action":  "delete",
			"task_id": 1.0,
		}
		msg, err := manageTasks(args, tmpDir)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "Task 1 deleted") {
			t.Errorf("Unexpected output: %s", msg)
		}

		tasks := readTasksFile()
		if len(tasks) != 0 {
			t.Errorf("Expected 0 tasks, got %d", len(tasks))
		}
	})

	// Test 5: Clear tasks
	t.Run("Clear", func(t *testing.T) {
		// Add a task first
		manageTasks(map[string]interface{}{"action": "add", "content": "To be cleared"}, tmpDir)
		
		args := map[string]interface{}{
			"action": "clear",
		}
		msg, err := manageTasks(args, tmpDir)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if msg != "All tasks cleared." {
			t.Errorf("Unexpected output: %s", msg)
		}

		tasks := readTasksFile()
		if len(tasks) != 0 {
			t.Errorf("Expected 0 tasks after clear, got %d", len(tasks))
		}
	})

	// Test 6: Persistence (New instance/call loads existing)
	t.Run("Persistence", func(t *testing.T) {
		// Manually write a file to simulate existing state
		initialTasks := []Task{
			{ID: 10, Content: "Persistent Task", Status: "pending"},
		}
		data, _ := json.Marshal(initialTasks)
		path := filepath.Join(tmpDir, "output", "global-tasks.json")
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, data, 0644)

		// List to verify it reads correctly
		args := map[string]interface{}{
			"action": "list",
		}
		msg, err := manageTasks(args, tmpDir)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "[10] [pending] Persistent Task") {
			t.Errorf("Persistence check failed. Output: %s", msg)
		}
	})
}
