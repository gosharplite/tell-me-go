// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestTaskRepository(t *testing.T) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	repo := NewTaskRepository(fs, tasksFile)

	t.Run("Save and Load Tasks", func(t *testing.T) {
		tasks := []services.Task{
			{ID: 1, Content: "Task 1", Status: "pending"},
			{ID: 2, Content: "Task 2", Status: "completed"},
		}

		if err := repo.WriteAll(ctx, tasks); err != nil {
			t.Fatal(err)
		}

		loaded, err := repo.ReadAll(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if len(loaded) != 2 {
			t.Errorf("expected 2 tasks, got %d", len(loaded))
		}
		if loaded[0].Content != "Task 1" || loaded[1].Content != "Task 2" {
			t.Error("tasks content mismatch")
		}
	})

	t.Run("Load Non-existent File", func(t *testing.T) {
		repo2 := NewTaskRepository(fs, filepath.Join(tempDir, "non-existent.json"))
		loaded, err := repo2.ReadAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if loaded != nil {
			t.Error("expected nil for non-existent file")
		}
	})

	t.Run("Verify JSONL Format", func(t *testing.T) {
		tasks := []services.Task{
			{ID: 1, Content: "Task 1"},
			{ID: 2, Content: "Task 2"},
		}
		if err := repo.WriteAll(ctx, tasks); err != nil {
			t.Fatal(err)
		}

		content, err := fs.ReadFile(ctx, tasksFile)
		if err != nil {
			t.Fatal(err)
		}

		// Count newlines
		count := 0
		for _, b := range content {
			if b == '\n' {
				count++
			}
		}
		if count != 2 {
			t.Errorf("expected 2 lines in JSONL, got %d", count)
		}

		// Test Append
		if err := repo.Append(ctx, services.Task{ID: 3, Content: "Task 3"}); err != nil {
			t.Fatal(err)
		}
		content, err = fs.ReadFile(ctx, tasksFile)
		if err != nil {
			t.Fatal(err)
		}
		count = 0
		for _, b := range content {
			if b == '\n' {
				count++
			}
		}
		if count != 3 {
			t.Errorf("expected 3 lines in JSONL after append, got %d", count)
		}
	})
}
