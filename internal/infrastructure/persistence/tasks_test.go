// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTaskRepository_LoadJSONL(t *testing.T) {
	t.Parallel()
	repo, ctx, tasksFile, _ := setupTaskRepo(t)
	fs := NewOSFileSystem()

	// Manually write JSONL
	content := `{"id": 1, "content": "Task 1"}
{"id": 2, "content": "Task 2"}
`
	if err := fs.WriteFile(ctx, tasksFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.ReadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(loaded))
	}
	if loaded[0].Content != "Task 1" || loaded[1].Content != "Task 2" {
		t.Error("tasks content mismatch")
	}
}

func TestTaskRepository_LoadNonExistent(t *testing.T) {
	t.Parallel()
	_, ctx, _, tempDir := setupTaskRepo(t)
	fs := NewOSFileSystem()

	repo2 := newTaskRepository(fs, filepath.Join(tempDir, "non-existent.json"))
	loaded, err := repo2.ReadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Error("expected nil for non-existent file")
	}
}

func setupTaskRepo(t *testing.T) (*taskRepository, context.Context, string, string) {
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	repo := newTaskRepository(fs, tasksFile)
	return repo, ctx, tasksFile, tempDir
}

func TestTaskRepository_JSONArrayCompatibility(t *testing.T) {
	t.Parallel()
	repo, ctx, tasksFile, _ := setupTaskRepo(t)
	fs := NewOSFileSystem()

	// Manually write a JSON array
	content := `[{"id": 1, "content": "Array Task"}]`
	if err := fs.WriteFile(ctx, tasksFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.ReadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 task, got %d", len(loaded))
	}
	if loaded[0].Content != "Array Task" {
		t.Errorf("expected 'Array Task', got %q", loaded[0].Content)
	}
}
