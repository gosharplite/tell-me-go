// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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

	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
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
	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
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
	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
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
	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
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
	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
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
	if err := migrateFromJSON(ctx, db, fs, tasksDir, slog.Default()); err != nil {
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

func TestRepository_BulkInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := NewOSFileSystem()

	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "bulk_tasks.json")
	dbPath := filepath.Join(tempDir, "bulk_test.db")

	// Generate 500 tasks
	tasksJSON := "["
	for i := 1; i <= 500; i++ {
		if i > 1 {
			tasksJSON += ","
		}
		tasksJSON += fmt.Sprintf(`{"id": %d, "content": "Bulk Task %d", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}`, i, i)
	}
	tasksJSON += "]"

	if err := fs.WriteFile(ctx, tasksFile, []byte(tasksJSON), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
		t.Fatalf("migrateFromJSON failed: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("QueryRowContext failed: %v", err)
	}
	if count != 500 {
		t.Errorf("Expected 500 tasks, got %d", count)
	}
}

// =============================================================================
// Additional error-path tests
// =============================================================================

func TestInitSQLiteDB_PragmaFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Create a regular file to use as a "parent" — sql.Open will succeed
	// because it's lazy, but the first ExecContext (pragma) will fail because
	// SQLite cannot create a DB file inside a path where a component is a
	// regular file, not a directory.
	blocker := filepath.Join(tmpDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(blocker, "sub", "test.db")

	_, err := initSQLiteDB(context.Background(), badPath)
	if err == nil {
		t.Error("expected error for invalid db path, got nil")
	}
}

func TestCreateTables_SecondQueryFailure(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	// Create the DB file by running one query, then close to force
	// ExecContext failure on the subsequent createTables call.
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS dummy (id INTEGER);"); err != nil {
		t.Fatalf("failed to create dummy table: %v", err)
	}
	_ = db.Close()

	err = createTables(context.Background(), db)
	if err == nil {
		t.Error("expected error from createTables with closed DB, got nil")
	}
}

func TestMigrateFromJSON_CountQueryFailure(t *testing.T) {
	t.Parallel()

	fs := NewOSFileSystem()
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.json")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a valid tasks file so the count query is the only blocker.
	if err := fs.WriteFile(context.Background(), tasksPath, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	_ = db.Close() // Close immediately to force query failure.

	err = migrateFromJSON(context.Background(), db, fs, tasksPath, slog.Default())
	if err == nil {
		t.Error("expected error from count query on closed DB, got nil")
	}
}

func TestMigrateTasks_TxBeginFailure(t *testing.T) {
	t.Parallel()

	fs := NewOSFileSystem()
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.json")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Write valid tasks so ReadAll succeeds.
	tasksJSON := `[{"id": 1, "content": "Test", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`
	if err := fs.WriteFile(context.Background(), tasksPath, []byte(tasksJSON), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	_ = db.Close() // Close to force BeginTx failure.

	err = migrateTasks(context.Background(), db, fs, tasksPath, slog.Default())
	if err == nil {
		t.Error("expected error from BeginTx on closed DB, got nil")
	}
}

func TestExecuteBatchInsert_PartialFailure(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create the tasks table.
	if _, err := db.Exec("CREATE TABLE tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Create more than 200 rows to trigger multi-batch execution.
	rows := make([]taskRow, 250)
	for i := range rows {
		rows[i] = taskRow{
			ID:        int64(i + 1),
			Content:   fmt.Sprintf("Task %d", i+1),
			Status:    "pending",
			CreatedAt: "2025-01-01T00:00:00Z",
		}
	}

	// Cancel the context to trigger ExecContext failure during batch insert.
	// This exercises the error return path inside the batch loop.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = executeBatchInsert(ctx, tx, rows)
	if err == nil {
		t.Error("expected error from cancelled context during batch insert, got nil")
	}
}
