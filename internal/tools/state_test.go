// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestManageTasks(t *testing.T) {
	// Create a temporary directory for homeDir
	tmpDir, err := os.MkdirTemp("", "tools_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testMode := "test_mode"
	ctx := context.Background()
	sm := NewSecurityManager()
	s := &stateManager{
		homeDir: tmpDir,
		mode:    testMode,
		sm:      sm,
	}

	// Helper to read the tasks file directly
	readTasksFile := func() []Task {
		path := filepath.Join(tmpDir, "output", testMode, "tasks.json")
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
		msg, err := s.manageTasks(ctx, args)
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
		msg, err := s.manageTasks(ctx, args)
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
		msg, err := s.manageTasks(ctx, args)
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
		msg, err := s.manageTasks(ctx, args)
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
		s.manageTasks(ctx, map[string]interface{}{"action": "add", "content": "To be cleared"})

		args := map[string]interface{}{
			"action": "clear",
		}
		msg, err := s.manageTasks(ctx, args)
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

	// Test 6: Persistence
	t.Run("Persistence", func(t *testing.T) {
		// Manually write a file to simulate existing state
		initialTasks := []Task{
			{ID: 10, Content: "Persistent Task", Status: "pending"},
		}
		data, _ := json.Marshal(initialTasks)
		path := s.getTasksPath()
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, data, 0644)

		// List to verify it reads correctly
		args := map[string]interface{}{
			"action": "list",
		}
		msg, err := s.manageTasks(ctx, args)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "[10] [pending] Persistent Task") {
			t.Errorf("Persistence check failed. Output: %s", msg)
		}
	})
}

func TestManageScratchpad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "scratchpad_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testMode := "test_mode"
	ctx := context.Background()
	s := &stateManager{
		homeDir: tmpDir,
		mode:    testMode,
	}
	scratchpadPath := s.getScratchpadPath()

	// Helper to read content
	readScratchpad := func() string {
		data, err := os.ReadFile(scratchpadPath)
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			t.Fatalf("Failed to read scratchpad: %v", err)
		}
		return string(data)
	}

	// Test 1: Read non-existent
	t.Run("ReadEmpty", func(t *testing.T) {
		args := map[string]interface{}{"action": "read"}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "[Scratchpad does not exist yet]" {
			t.Errorf("Expected '[Scratchpad does not exist yet]', got: %q", msg)
		}
	})

	// Test 2: Write
	t.Run("Write", func(t *testing.T) {
		args := map[string]interface{}{
			"action":  "write",
			"content": "# Plan\n- Step 1",
		}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "Scratchpad overwritten." {
			t.Errorf("Unexpected output: %s", msg)
		}

		content := readScratchpad()
		if content != "# Plan\n- Step 1" {
			t.Errorf("Content mismatch: %q", content)
		}
	})

	// Test 3: Read Existing
	t.Run("ReadExisting", func(t *testing.T) {
		args := map[string]interface{}{"action": "read"}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "# Plan\n- Step 1" {
			t.Errorf("Content mismatch on read: %q", msg)
		}
	})

	// Test 4: Append
	t.Run("Append", func(t *testing.T) {
		args := map[string]interface{}{
			"action":  "append",
			"content": "- Step 2",
		}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "Content appended to scratchpad." {
			t.Errorf("Unexpected output: %s", msg)
		}

		content := readScratchpad()
		expected := "# Plan\n- Step 1\n- Step 2"
		if content != expected {
			t.Errorf("Append content mismatch.\nExpected:\n%q\nGot:\n%q", expected, content)
		}
	})

	// Test 5: Append to New File
	t.Run("AppendNew", func(t *testing.T) {
		// Clean up first
		os.Remove(scratchpadPath)

		args := map[string]interface{}{
			"action":  "append",
			"content": "New Note",
		}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "Content appended to scratchpad." {
			t.Errorf("Unexpected output: %s", msg)
		}

		content := readScratchpad()
		if content != "New Note" {
			t.Errorf("Append new content mismatch: %q", content)
		}
	})

	// Test 6: Clear
	t.Run("Clear", func(t *testing.T) {
		args := map[string]interface{}{"action": "clear"}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "Scratchpad cleared." {
			t.Errorf("Unexpected output: %s", msg)
		}

		content := readScratchpad()
		if content != "" {
			t.Errorf("Expected empty file, got: %q", content)
		}
	})
}

func TestStateConcurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "concurrency_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testMode := "concurrent"
	ctx := context.Background()
	sm := NewSecurityManager()
	s := &stateManager{
		homeDir: tmpDir,
		mode:    testMode,
		sm:      sm,
	}

	numGroutines := 20
	tasksPerRoutine := 10

	var wg sync.WaitGroup
	wg.Add(numGroutines)

	for i := 0; i < numGroutines; i++ {
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < tasksPerRoutine; j++ {
				content := fmt.Sprintf("Task from %d index %d", routineID, j)
				_, err := s.manageTasks(ctx, map[string]interface{}{
					"action":  "add",
					"content": content,
				})
				if err != nil {
					t.Errorf("manageTasks add failed: %v", err)
				}

				// Also do some scratchpad appends
				_, err = s.manageScratchpad(ctx, map[string]interface{}{
					"action":  "append",
					"content": fmt.Sprintf("Log from %d-%d", routineID, j),
				})
				if err != nil {
					t.Errorf("manageScratchpad append failed: %v", err)
				}

				// Also do config sets
				_, err = s.manageConfig(ctx, map[string]interface{}{
					"action": "set",
					"key":    fmt.Sprintf("key-%d-%d", routineID, j),
					"value":  "val",
				})
				if err != nil {
					t.Errorf("manageConfig set failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify total tasks
	msg, err := s.manageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("manageTasks list failed: %v", err)
	}

	expectedTotal := numGroutines * tasksPerRoutine

	count := 0
	for _, line := range strings.Split(msg, "\n") {
		if strings.HasPrefix(line, "[") {
			count++
		}
	}

	if count != expectedTotal {
		t.Errorf("Expected %d tasks, got %d. Output:\n%s", expectedTotal, count, msg)
	}
}

func TestCorruptionRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "corruption_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testMode := "corrupt"
	ctx := context.Background()
	sm := NewSecurityManager()
	s := &stateManager{
		homeDir: tmpDir,
		mode:    testMode,
		sm:      sm,
	}

	path := s.getTasksPath()
	os.MkdirAll(filepath.Dir(path), 0755)

	// Write invalid JSON
	os.WriteFile(path, []byte("{ invalid json ["), 0644)

	// Try to add a task. It should reset and succeed.
	msg, err := s.manageTasks(ctx, map[string]interface{}{
		"action":  "add",
		"content": "Recovery Task",
	})

	if err != nil {
		t.Fatalf("manageTasks failed after corruption: %v", err)
	}
	if !strings.Contains(msg, "Task added with ID: 1") {
		t.Errorf("Expected recovery and ID 1, got: %s", msg)
	}

	// Verify .bak file exists
	if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
		t.Errorf("Expected corruption backup file %s.bak to exist", path)
	}

	// Verify list works
	msg, err = s.manageTasks(ctx, map[string]interface{}{"action": "list"})
	if err != nil {
		t.Fatalf("manageTasks list failed after recovery: %v", err)
	}
	if !strings.Contains(msg, "[1] [pending] Recovery Task") {
		t.Errorf("List missing recovered task: %s", msg)
	}
}
