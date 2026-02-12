// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestTaskRepository_SaveAndLoad(t *testing.T) {
	repo, ctx, _, _ := setupTaskRepo(t)

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
}

func TestTaskRepository_LoadNonExistent(t *testing.T) {
	_, ctx, _, tempDir := setupTaskRepo(t)
	fs := storage.DefaultFileSystem

	repo2 := NewTaskRepository(fs, filepath.Join(tempDir, "non-existent.json"))
	loaded, err := repo2.ReadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Error("expected nil for non-existent file")
	}
}

func TestTaskRepository_JSONLFormat(t *testing.T) {
	repo, ctx, tasksFile, _ := setupTaskRepo(t)
	fs := storage.DefaultFileSystem

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

	count := bytes.Count(content, []byte("\n"))
	if count != 2 {
		t.Errorf("expected 2 lines in JSONL, got %d", count)
	}
}

func TestTaskRepository_Append(t *testing.T) {
	repo, ctx, tasksFile, _ := setupTaskRepo(t)
	fs := storage.DefaultFileSystem

	tasks := []services.Task{
		{ID: 1, Content: "Task 1"},
		{ID: 2, Content: "Task 2"},
	}
	if err := repo.WriteAll(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	if err := repo.Append(ctx, services.Task{ID: 3, Content: "Task 3"}); err != nil {
		t.Fatal(err)
	}

	content, err := fs.ReadFile(ctx, tasksFile)
	if err != nil {
		t.Fatal(err)
	}

	count := bytes.Count(content, []byte("\n"))
	if count != 3 {
		t.Errorf("expected 3 lines in JSONL after append, got %d", count)
	}
}

func setupTaskRepo(t *testing.T) (*TaskRepository, context.Context, string, string) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	repo := NewTaskRepository(fs, tasksFile)
	return repo, ctx, tasksFile, tempDir
}
