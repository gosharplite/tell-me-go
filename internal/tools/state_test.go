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

func createTestStateManager(t *testing.T, tmpDir string) *stateManager {
	sm := NewSecurityManager()
	// Allow tmpDir in security manager for tests
	sm.RegisterSafePath(tmpDir)
	
	s := &stateManager{
		sm:          sm,
		tasks:       make(map[float64]Task),
		taskNextID:  1,
		config:      make(map[string]string),
		configFile:  filepath.Join(tmpDir, "config.json"),
		scratchFile: filepath.Join(tmpDir, "scratchpad.md"),
		tasksFile:   filepath.Join(tmpDir, "tasks.json"),
	}
	s.initSessionInfo(tmpDir)
	return s
}

func TestManageTasks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tools_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	s := createTestStateManager(t, tmpDir)

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
		if !strings.Contains(msg, "Task added with ID 1") {
			t.Errorf("Unexpected output: %s", msg)
		}

		// Verify file persistence
		data, err := os.ReadFile(s.tasksFile)
		if err != nil {
			t.Fatalf("Failed to read tasks file: %v", err)
		}
		var tasks []Task
		if err := json.Unmarshal(data, &tasks); err != nil {
			t.Fatalf("Failed to unmarshal tasks: %v", err)
		}
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
		if !strings.Contains(msg, "1. [ ] First Task (pending)") {
			t.Errorf("List output missing task: %s", msg)
		}
	})

	// Test 3: Update task
	t.Run("Update", func(t *testing.T) {
		args := map[string]interface{}{
			"action":  "update",
			"task_id": 1.0,
			"status":  "completed",
		}
		msg, err := s.manageTasks(ctx, args)
		if err != nil {
			t.Fatalf("manageTasks failed: %v", err)
		}
		if !strings.Contains(msg, "Task 1 updated") {
			t.Errorf("Unexpected output: %s", msg)
		}

		s.loadTasks() // Reload from disk to verify persistence
		if s.tasks[1].Status != "completed" {
			t.Errorf("Task status not updated: %s", s.tasks[1].Status)
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

		s.loadTasks()
		if len(s.tasks) != 0 {
			t.Errorf("Expected 0 tasks, got %d", len(s.tasks))
		}
	})

	// Test 5: Clear tasks
	t.Run("Clear", func(t *testing.T) {
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

		s.loadTasks()
		if len(s.tasks) != 0 {
			t.Errorf("Expected 0 tasks after clear, got %d", len(s.tasks))
		}
	})
}

func TestManageScratchpad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "scratchpad_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	s := createTestStateManager(t, tmpDir)

	// Test 1: Read non-existent
	t.Run("ReadEmpty", func(t *testing.T) {
		args := map[string]interface{}{"action": "read"}
		msg, err := s.manageScratchpad(ctx, args)
		if err != nil {
			t.Fatalf("manageScratchpad failed: %v", err)
		}
		if msg != "(Scratchpad is empty)" {
			t.Errorf("Expected '(Scratchpad is empty)', got: %q", msg)
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
		if msg != "Scratchpad updated." {
			t.Errorf("Unexpected output: %s", msg)
		}

		content, _ := os.ReadFile(s.scratchFile)
		if string(content) != "# Plan\n- Step 1" {
			t.Errorf("Content mismatch: %q", content)
		}
	})

	// Test 3: Read Existing
	t.Run("ReadExisting", func(t *testing.T) {
		// Verify internal state matches file
		s.loadScratchpad() 
		
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

		content, _ := os.ReadFile(s.scratchFile)
		expected := "# Plan\n- Step 1\n- Step 2"
		if string(content) != expected {
			t.Errorf("Append content mismatch.\nExpected:\n%q\nGot:\n%q", expected, content)
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

		content, _ := os.ReadFile(s.scratchFile)
		if len(content) != 0 {
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

	ctx := context.Background()
	s := createTestStateManager(t, tmpDir)

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
	s.loadTasks() // Reload to verify persistence
	if len(s.tasks) != numGroutines*tasksPerRoutine {
		t.Errorf("Expected %d tasks, got %d", numGroutines*tasksPerRoutine, len(s.tasks))
	}
}
