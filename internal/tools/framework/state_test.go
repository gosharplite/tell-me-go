// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestStateManager(t *testing.T) {
	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)

	m := &stateManager{
		sm:         sm,
		tasks:      NewTaskStore(filepath.Join(tempDir, "tasks.json")),
		config:     NewConfigStore(filepath.Join(tempDir, "config.json")),
		scratchpad: NewScratchpadStore(filepath.Join(tempDir, "scratchpad.md")),
	}

	ctx := context.Background()

	t.Run("Write and Read Scratchpad", func(t *testing.T) {
		content := "Initial thoughts."
		_, err := m.scratchpad.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": content,
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := m.scratchpad.ManageScratchpad(ctx, map[string]interface{}{
			"action": "read",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, content) {
			t.Errorf("got %q, want %q", res.Text, content)
		}
	})

	t.Run("Append Scratchpad", func(t *testing.T) {
		addition := "\nMore thoughts."
		_, err := m.scratchpad.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "append",
			"content": addition,
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := m.scratchpad.ManageScratchpad(ctx, map[string]interface{}{
			"action": "read",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "Initial thoughts.") || !strings.Contains(res.Text, "More thoughts.") {
			t.Errorf("scratchpad missing expected content: %s", res.Text)
		}
	})

	t.Run("Manage Tasks", func(t *testing.T) {
		// Add task
		_, err := m.tasks.ManageTasks(ctx, map[string]interface{}{
			"action":  "add",
			"content": "Implement feature X",
		})
		if err != nil {
			t.Fatal(err)
		}

		// List tasks
		res, err := m.tasks.ManageTasks(ctx, map[string]interface{}{
			"action": "list",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "Implement feature X") {
			t.Errorf("task list missing new task: %s", res.Text)
		}

		// Update task
		_, err = m.tasks.ManageTasks(ctx, map[string]interface{}{
			"action":  "update",
			"task_id": 1.0,
			"status":  "completed",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Verify update
		res, _ = m.tasks.ManageTasks(ctx, map[string]interface{}{"action": "list"})
		if !strings.Contains(res.Text, "[x]") {
			t.Errorf("task status not updated: %s", res.Text)
		}
	})

	t.Run("Manage Config", func(t *testing.T) {
		_, err := m.config.ManageConfig(ctx, map[string]interface{}{
			"action": "set",
			"key":    "theme",
			"value":  "dark",
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := m.config.ManageConfig(ctx, map[string]interface{}{
			"action": "get",
			"key":    "theme",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "dark") {
			t.Errorf("config get failed: %s", res.Text)
		}
	})

	t.Run("Persistence", func(t *testing.T) {
		// Create a new manager pointing to the same directory
		configStore2 := NewConfigStore(filepath.Join(tempDir, "config.json"))
		err := configStore2.Load()
		if err != nil {
			t.Fatal(err)
		}

		res, err := configStore2.ManageConfig(ctx, map[string]interface{}{
			"action": "get",
			"key":    "theme",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "dark") {
			t.Error("state was not persisted to disk")
		}
	})
}
