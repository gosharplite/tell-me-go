// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
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

	repo2 := newTaskRepository(fs, filepath.Join(tempDir, "non-existent.json"), nil)
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
	repo := newTaskRepository(fs, tasksFile, nil)
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

func TestTaskRepository_CorruptedLine(t *testing.T) {
	// Set the env var that gates the debug log.
	t.Setenv("TELL_ME_DEBUG", "migration")

	var buf testfixtures.SyncWriter
	testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repo, ctx, tasksFile, _ := setupTaskRepoWithLogger(t, testLogger)
	fs := NewOSFileSystem()

	// Write mixed content: one valid JSONL line, one corrupted.
	content := `{"id": 1, "content": "Task 1"}
this is not json
{"id": 2, "content": "Task 2"}
`
	if err := fs.WriteFile(ctx, tasksFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := repo.ReadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The corrupted line is skipped; only valid tasks are returned.
	if len(loaded) != 2 {
		t.Fatalf("expected 2 tasks (corrupted line skipped), got %d", len(loaded))
	}
	if loaded[0].Content != "Task 1" || loaded[1].Content != "Task 2" {
		t.Error("tasks content mismatch")
	}

	// Verify the debug message was logged.
	output := buf.String()
	if !strings.Contains(output, "corrupted task line during migration") {
		t.Error("expected debug log for corrupted task line")
	}
}

func setupTaskRepoWithLogger(t *testing.T, logger *slog.Logger) (*taskRepository, context.Context, string, string) {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	repo := newTaskRepository(NewOSFileSystem(), tasksFile, logger)
	return repo, ctx, tasksFile, tempDir
}

func TestTaskRepository_ReadFileError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()

	// Create a DIRECTORY at the tasks file path.
	// Stat will succeed (directory exists), but ReadFile will fail.
	tasksFile := filepath.Join(tempDir, "tasks_is_a_dir")
	if err := os.MkdirAll(tasksFile, 0755); err != nil {
		t.Fatal(err)
	}

	fs := NewOSFileSystem()
	repo := newTaskRepository(fs, tasksFile, nil)

	_, err := repo.ReadAll(ctx)
	if err == nil {
		t.Fatal("expected error when reading a directory as a tasks file, got nil")
	}
	if !strings.Contains(err.Error(), "reading tasks file") {
		t.Errorf("expected error to contain 'reading tasks file', got: %v", err)
	}
}

func TestTaskRepository_EmptyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	fs := NewOSFileSystem()

	// Write an empty file
	if err := fs.WriteFile(ctx, tasksFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	repo := newTaskRepository(fs, tasksFile, nil)
	tasks, err := repo.ReadAll(ctx)
	if err != nil {
		t.Fatalf("ReadAll should not error on empty file: %v", err)
	}
	if tasks != nil {
		t.Errorf("expected nil tasks for empty file, got %v", tasks)
	}
}

func TestTaskRepository_JSONArrayUnmarshalFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	fs := NewOSFileSystem()

	// Content starts with '[' but is NOT valid JSON — not an array, not an object.
	// This triggers the json.Unmarshal failure path, falling through to JSONL parsing.
	content := `[this is not valid json at all`
	if err := fs.WriteFile(ctx, tasksFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	repo := newTaskRepository(fs, tasksFile, nil)
	tasks, err := repo.ReadAll(ctx)
	if err != nil {
		t.Fatalf("ReadAll should not error on fallthrough to JSONL: %v", err)
	}
	// JSONL parsing finds zero valid lines, so result is empty.
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks from invalid JSON array, got %d", len(tasks))
	}
}

func TestParseJSONLTasks_BlankLine(t *testing.T) {
	t.Parallel()

	trimmed := `{"id": 1, "content": "Task 1"}

{"id": 2, "content": "Task 2"}
`
	result := parseJSONLTasks(trimmed, "/test/tasks.json", slog.Default())
	if len(result) != 2 {
		t.Fatalf("expected 2 tasks (blank line skipped), got %d", len(result))
	}
}

func TestParseJSONLTasks_CorruptedLineNoDebug(t *testing.T) {
	// NOTE: t.Parallel() is incompatible with t.Setenv(); this test runs serially.

	// Ensure TELL_ME_DEBUG does NOT contain "migration"
	t.Setenv("TELL_ME_DEBUG", "")

	var buf testfixtures.SyncWriter
	testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	trimmed := `{"id": 1, "content": "Task 1"}
not valid json
{"id": 2, "content": "Task 2"}
`
	result := parseJSONLTasks(trimmed, "/test/tasks.json", testLogger)
	if len(result) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result))
	}

	// Verify NO debug message was logged (since TELL_ME_DEBUG doesn't contain migration)
	output := buf.String()
	if strings.Contains(output, "corrupted task line during migration") {
		t.Error("expected no debug log when TELL_ME_DEBUG does not contain 'migration'")
	}
}
