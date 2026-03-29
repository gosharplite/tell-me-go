// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	_ "modernc.org/sqlite"
)

func TestSQLiteMigrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := NewOSFileSystem()

	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	dbPath := filepath.Join(tempDir, "test.db")

	seedLegacyFiles(t, ctx, fs, tasksFile)

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrateFromJSON(ctx, db, fs, tasksFile); err != nil {
		t.Fatalf("migrateFromJSON failed: %v", err)
	}

	t.Run("Tasks Migration", func(t *testing.T) { t.Parallel(); testTasksMigration(t, ctx, db) })
	t.Run("Idempotency", func(t *testing.T) {
		t.Parallel()
		testMigrationIdempotency(t, ctx, db, fs, tasksFile)
	})
}

func testTasksMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var taskContent string
	if err := db.QueryRowContext(ctx, "SELECT content FROM tasks WHERE id = 1").Scan(&taskContent); err != nil {
		t.Errorf("Failed to read migrated task: %v", err)
	} else if taskContent != "Migrated Task 1" {
		t.Errorf("Task migration mismatch: expected 'Migrated Task 1', got %q", taskContent)
	}
}

func testMigrationIdempotency(t *testing.T, ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksFile string) {
	t.Helper()
	if err := migrateFromJSON(ctx, db, fs, tasksFile); err != nil {
		t.Fatalf("migrateFromJSON second run failed: %v", err)
	}
}

func seedLegacyFiles(t *testing.T, ctx context.Context, fs persistence.FileSystem, tasksFile string) {
	t.Helper()
	tasksJSON := `[{"id": 1, "content": "Migrated Task 1", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`

	if err := fs.WriteFile(ctx, tasksFile, []byte(tasksJSON), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteMigrations_MissingFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := NewOSFileSystem()

	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "missing_tasks.json")
	dbPath := filepath.Join(tempDir, "test2.db")

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Migration should not fail if files are missing
	if err := migrateFromJSON(ctx, db, fs, tasksFile); err != nil {
		t.Fatalf("migrateFromJSON failed with missing files: %v", err)
	}
}

func TestSQLiteMigrations_CorruptedData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := NewOSFileSystem()

	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "tasks.json")
	dbPath := filepath.Join(tempDir, "test3.db")

	// 1. Binary garbage/invalid json
	if err := fs.WriteFile(ctx, tasksFile, []byte("{invalid json garbage \x00\x01\x02"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Migration should log an error but not fail the boot process
	if err := migrateFromJSON(ctx, db, fs, tasksFile); err != nil {
		t.Fatalf("migrateFromJSON failed with invalid json: %v", err)
	}

	// Verify table is empty but exists
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 tasks, got %d", count)
	}

	// Idempotency: second call should still work
	if err := migrateFromJSON(ctx, db, fs, tasksFile); err != nil {
		t.Fatalf("migrateFromJSON second run failed: %v", err)
	}
}

func TestSQLiteMigrations_DirectoryAsFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := NewOSFileSystem()

	tempDir := t.TempDir()
	tasksDir := filepath.Join(tempDir, "tasks_is_dir")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(tempDir, "test4.db")

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Migration should handle directory as file path gracefully
	if err := migrateFromJSON(ctx, db, fs, tasksDir); err != nil {
		t.Fatalf("migrateFromJSON failed with directory as path: %v", err)
	}
}

func TestSQLiteMigrations_InvalidDBPath(t *testing.T) {
	t.Parallel()
	_, err := initSQLiteDB(context.Background(), "/invalid/path/db.sqlite")
	if err == nil {
		t.Error("expected error for invalid db path")
	}
}
