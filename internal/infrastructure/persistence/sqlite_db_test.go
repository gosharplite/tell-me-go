// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Corrupted data: the JSONL fallback in readAllInternal skips the
	// unparseable line, returning 0 tasks with no error. migrateTasks sees
	// len(tasks)==0 and returns nil. migrateFromJSON therefore returns nil
	// — this is correct; corrupted data is gracefully handled as a no-op.
	if err := migrateFromJSON(ctx, db, fs, tasksFile, slog.Default()); err != nil {
		t.Fatalf("migrateFromJSON should handle corrupted data gracefully: %v", err)
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

func TestMigrateFromJSON_MigrateTasksError(t *testing.T) {
	// Platform-sensitive: mode 0000 reliably causes ReadFile to fail with EACCES on Linux/macOS.
	if runtime.GOOS == "windows" {
		t.Skip("file permission test not reliable on Windows")
	}

	ctx := context.Background()
	fs := NewOSFileSystem()
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "tasks.json")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a valid-looking tasks file with mode 0000 — no read permission.
	if err := os.WriteFile(tasksPath, []byte(`[{"id":1,"content":"T","status":"pending","created_at":"2025-01-01T00:00:00Z"}]`), 0000); err != nil {
		t.Fatal(err)
	}
	// Restore permission at test end so TempDir cleanup works.
	defer func() { _ = os.Chmod(tasksPath, 0644) }()

	// Open DB and create tasks table — COUNT query must succeed (returns 0).
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);"); err != nil {
		t.Fatal(err)
	}

	// migrateFromJSON: COUNT returns 0 → calls migrateTasks
	// migrateTasks: Stat succeeds (file exists, IsDir=false) → ReadFile fails (EACCES)
	// → migrateTasks returns error → migrateFromJSON returns error
	err = migrateFromJSON(ctx, db, fs, tasksPath, slog.Default())
	if err == nil {
		t.Fatal("migrateFromJSON should return error when migrateTasks fails")
	}
}

// TestMigrateTasks_RollbackWarning (below) covers the tx.Rollback() defer
// warning branch in migrateTasks using a custom driver.Connector wrapper.
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
	defer func() { _ = db.Close() }()

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

func TestMigrateTasks_EmptyTasksArray(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := NewOSFileSystem()
	tmpDir := t.TempDir()
	tasksPath := filepath.Join(tmpDir, "empty_tasks.json")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Write a valid JSON file containing an empty array
	if err := fs.WriteFile(ctx, tasksPath, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// migrateTasks reads the empty array, gets len(tasks)==0, returns nil early
	err = migrateTasks(ctx, db, fs, tasksPath, slog.Default())
	if err != nil {
		t.Fatalf("migrateTasks with empty array returned error: %v", err)
	}
}

// =============================================================================
// TestInitSQLiteDB_ErrorPaths — coverage for NewSQLiteDB constructor error branches
// =============================================================================

func TestInitSQLiteDB_ErrorPaths(t *testing.T) {

	t.Run("sqlOpen_failure", func(t *testing.T) {

		origOpen := sqlOpenFn
		t.Cleanup(func() { sqlOpenFn = origOpen })

		sqlOpenFn = func(driverName, dsn string) (*sql.DB, error) {
			return nil, fmt.Errorf("injected open failure")
		}

		_, err := initSQLiteDB(context.Background(), "/some/path")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open sqlite db")
		assert.Contains(t, err.Error(), "injected open failure")
	})

	t.Run("createTables_failure", func(t *testing.T) {

		db, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		// Make the database read-only so that ExecContext calls fail.
		_, err = db.Exec("PRAGMA query_only = 1")
		require.NoError(t, err)

		err = createTables(context.Background(), db)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "executing schema query")
	})

	t.Run("initSQLiteDB_createTables_failure", func(t *testing.T) {

		dbPath := filepath.Join(t.TempDir(), "createtables_fail.db")
		origOpen := sqlOpenFn
		t.Cleanup(func() { sqlOpenFn = origOpen })
		sqlOpenFn = func(driverName, dsn string) (*sql.DB, error) {
			db, err := sql.Open(driverName, dsn)
			if err != nil {
				return nil, err
			}
			// Enable query_only mode before returning the handle.
			// Pragmas (journal_mode, busy_timeout) are connection-level
			// settings and still succeed, but CREATE TABLE is a schema
			// modification which is rejected in query_only mode.
			// This deterministically triggers the createTables error path.
			if _, err := db.Exec("PRAGMA query_only = 1"); err != nil {
				_ = db.Close()
				return nil, err
			}
			return db, nil
		}
		_, err := initSQLiteDB(context.Background(), dbPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create tables")
	})

	t.Run("executeBatchInsert_failure", func(t *testing.T) {

		db, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		// Create the tasks table so the INSERT statement itself is valid.
		_, err = db.Exec("CREATE TABLE tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);")
		require.NoError(t, err)

		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		rows := []taskRow{
			{ID: 1, Content: "Test", Status: "pending", CreatedAt: "2025-01-01T00:00:00Z"},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = executeBatchInsert(ctx, tx, rows)
		require.Error(t, err)
	})
}

// =============================================================================
// Task 3: Migration failure escalation tests
// =============================================================================

func TestMigrateFromJSON_MigrationFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) (db *sql.DB, fs persistence.FileSystem, tasksPath string)
		wantErr string
	}{
		{
			name: "no legacy file",
			setup: func(t *testing.T) (*sql.DB, persistence.FileSystem, string) {
				t.Helper()
				dir := t.TempDir()
				dbPath := filepath.Join(dir, "test.db")
				db, err := sql.Open("sqlite", dbPath)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })

				_, err = db.Exec("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);")
				require.NoError(t, err)

				return db, NewOSFileSystem(), filepath.Join(dir, "nonexistent.json")
			},
			wantErr: "", // nil is success — no-op
		},
		{
			name: "already migrated",
			setup: func(t *testing.T) (*sql.DB, persistence.FileSystem, string) {
				t.Helper()
				dir := t.TempDir()
				dbPath := filepath.Join(dir, "test.db")
				db, err := sql.Open("sqlite", dbPath)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })

				_, err = db.Exec("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);")
				require.NoError(t, err)
				_, err = db.Exec("INSERT INTO tasks (id, content, status, created_at) VALUES (1, 'Existing', 'pending', '2025-01-01T00:00:00Z');")
				require.NoError(t, err)

				return db, NewOSFileSystem(), filepath.Join(dir, "irrelevant.json")
			},
			wantErr: "", // nil is success — skip
		},
		{
			name: "migration fails mid-insert",
			setup: func(t *testing.T) (*sql.DB, persistence.FileSystem, string) {
				t.Helper()
				dir := t.TempDir()
				tasksPath := filepath.Join(dir, "tasks.json")
				dbPath := filepath.Join(dir, "test.db")

				// Write valid JSON that migrateTasks can parse.
				tasksJSON := `[{"id": 1, "content": "Task", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`
				require.NoError(t, os.WriteFile(tasksPath, []byte(tasksJSON), 0644))

				db, err := sql.Open("sqlite", dbPath)
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })

				// Create tasks table missing the created_at column so the INSERT
				// statement in executeBatchInsert fails with a column mismatch.
				_, err = db.Exec("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL);")
				require.NoError(t, err)

				return db, NewOSFileSystem(), tasksPath
			},
			wantErr: "migrating legacy tasks",
		},
		{
			name: "check count query fails",
			setup: func(t *testing.T) (*sql.DB, persistence.FileSystem, string) {
				t.Helper()
				dir := t.TempDir()
				dbPath := filepath.Join(dir, "test.db")
				db, err := sql.Open("sqlite", dbPath)
				require.NoError(t, err)
				_ = db.Close() // closed DB forces query failure

				return db, NewOSFileSystem(), filepath.Join(dir, "unused.json")
			},
			wantErr: "checking tasks table",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db, fs, tasksPath := tt.setup(t)

			err := migrateFromJSON(context.Background(), db, fs, tasksPath, slog.Default())

			if tt.wantErr == "" {
				assert.NoError(t, err, "expected no error")
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestMigrateTasks_RollbackOnError(t *testing.T) {
	t.Parallel()

	t.Run("batch insert fails", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		fs := NewOSFileSystem()
		dir := t.TempDir()
		tasksPath := filepath.Join(dir, "tasks.json")
		dbPath := filepath.Join(dir, "test.db")

		// Write valid JSON — parse succeeds.
		tasksJSON := `[{"id": 1, "content": "Task", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`
		require.NoError(t, os.WriteFile(tasksPath, []byte(tasksJSON), 0644))

		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		// Deliberately skip CREATE TABLE tasks — the INSERT in
		// executeBatchInsert will fail with "no such table: tasks".
		err = migrateTasks(ctx, db, fs, tasksPath, slog.Default())
		require.Error(t, err, "expected error from batch insert with missing table")

		// Transaction should be rolled back.
		// Now create the table and verify it's empty.
		_, err = db.Exec("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);")
		require.NoError(t, err)

		var count int
		err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM tasks").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "expected 0 tasks after rollback")
	})

	t.Run("successful migration", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		fs := NewOSFileSystem()
		dir := t.TempDir()
		tasksPath := filepath.Join(dir, "tasks.json")
		dbPath := filepath.Join(dir, "test.db")

		tasksJSON := `[{"id": 1, "content": "Task", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`
		require.NoError(t, os.WriteFile(tasksPath, []byte(tasksJSON), 0644))

		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		_, err = db.Exec("CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);")
		require.NoError(t, err)

		err = migrateTasks(ctx, db, fs, tasksPath, slog.Default())
		require.NoError(t, err)

		// Transaction committed; DB should contain the task.
		var count int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "expected 1 task after successful migration")
	})
}

func TestExecuteBatchInsert_ErrorContext(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Deliberately skip CREATE TABLE — the INSERT will fail with
	// "no such table: tasks", and the error message must include the batch range.
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// Create 250 rows to trigger multi-batch (batch 0-199, batch 200-249).
	rows := make([]taskRow, 250)
	for i := range rows {
		rows[i] = taskRow{
			ID:        int64(i + 1),
			Content:   fmt.Sprintf("Task %d", i+1),
			Status:    "pending",
			CreatedAt: "2025-01-01T00:00:00Z",
		}
	}

	err = executeBatchInsert(context.Background(), tx, rows)
	require.Error(t, err, "expected error from batch insert with missing table")
	assert.Contains(t, err.Error(), "batch 0-200", "error should mention batch range")
}

// =============================================================================
// rollbackFailingTx — driver.Tx wrapper whose Rollback() returns an injected
// error after performing a best-effort real rollback (to avoid resource leaks).
// =============================================================================

type rollbackFailingTx struct {
	driver.Tx
	rollbackErr error
}

func (tx *rollbackFailingTx) Rollback() error {
	_ = tx.Tx.Rollback() // best-effort real rollback to avoid resource leaks
	return tx.rollbackErr
}

// =============================================================================
// rollbackFailingConnector — driver.Connector that injects a Rollback error.
// Connect() opens a real sqlite connection and wraps it in rollbackFailingConn,
// which returns *rollbackFailingTx from BeginTx.
// =============================================================================

type rollbackFailingConnector struct {
	dbPath      string
	rollbackErr error
}

func (c *rollbackFailingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	realConn, err := sqliteDriver.Open(c.dbPath)
	if err != nil {
		return nil, err
	}
	return &rollbackFailingConn{
		conn:        realConn,
		rollbackErr: c.rollbackErr,
	}, nil
}

func (c *rollbackFailingConnector) Driver() driver.Driver {
	return sqliteDriver
}

// rollbackFailingConn wraps a real driver.Conn and returns *rollbackFailingTx
// from BeginTx. All other methods delegate to the real connection.
type rollbackFailingConn struct {
	conn        driver.Conn
	rollbackErr error
}

func (c *rollbackFailingConn) Prepare(query string) (driver.Stmt, error) {
	return c.conn.Prepare(query)
}

func (c *rollbackFailingConn) Close() error { return c.conn.Close() }

func (c *rollbackFailingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *rollbackFailingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bc, ok := c.conn.(driver.ConnBeginTx); ok {
		tx, err := bc.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &rollbackFailingTx{Tx: tx, rollbackErr: c.rollbackErr}, nil
	}
	// Fallback for drivers that don't implement ConnBeginTx.
	tx, err := c.conn.Begin() //nolint:staticcheck // SA1019: fallback for older drivers
	if err != nil {
		return nil, err
	}
	return &rollbackFailingTx{Tx: tx, rollbackErr: c.rollbackErr}, nil
}

func (c *rollbackFailingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if qc, ok := c.conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *rollbackFailingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if ec, ok := c.conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// =============================================================================
// TestMigrateTasks_RollbackWarning — covers the deferred tx.Rollback() warning
// branch in migrateTasks (sqlite_db.go L108-109). Tests two scenarios:
//   1. Rollback returns a non-ErrTxDone error → warning is logged
//   2. Rollback returns sql.ErrTxDone → warning is suppressed
// =============================================================================

func TestMigrateTasks_RollbackWarning(t *testing.T) {
	tests := []struct {
		name           string
		rollbackErr    error
		wantMigrateErr string // empty = expect nil
		wantLog        string // empty = expect NOT in logs
	}{
		{
			name:           "non-ErrTxDone rollback error logged",
			rollbackErr:    errors.New("disk I/O error during rollback"),
			wantMigrateErr: "migrating legacy tasks",
			wantLog:        "failed to rollback migration transaction",
		},
		{
			name:           "ErrTxDone suppressed",
			rollbackErr:    sql.ErrTxDone,
			wantMigrateErr: "", // nil — success
			wantLog:        "", // must NOT appear
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			dbPath := filepath.Join(dir, "test.db")
			tasksPath := filepath.Join(dir, "tasks.json")

			// Write valid tasks JSON.
			tasksJSON := `[{"id": 1, "content": "Task", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`
			require.NoError(t, os.WriteFile(tasksPath, []byte(tasksJSON), 0644))

			// Create connector with injected rollback error.
			connector := &rollbackFailingConnector{
				dbPath:      dbPath,
				rollbackErr: tt.rollbackErr,
			}
			db := sql.OpenDB(connector)
			t.Cleanup(func() { _ = db.Close() })

			// Create tasks table. For the non-ErrTxDone case, omit created_at
			// so executeBatchInsert fails. For the ErrTxDone case, include it.
			var createSQL string
			if tt.wantMigrateErr != "" {
				// Missing created_at → INSERT fails → Rollback with injected error
				createSQL = "CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL);"
			} else {
				createSQL = "CREATE TABLE IF NOT EXISTS tasks (id INTEGER PRIMARY KEY, content TEXT NOT NULL, status TEXT NOT NULL, created_at DATETIME NOT NULL);"
			}
			_, err := db.Exec(createSQL)
			require.NoError(t, err)

			// Set up logger with SyncWriter to capture output.
			var buf testfixtures.SyncWriter
			testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

			fs := NewOSFileSystem()
			ctx := context.Background()

			err = migrateFromJSON(ctx, db, fs, tasksPath, testLogger)

			output := buf.String()

			if tt.wantMigrateErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMigrateErr)
			}

			if tt.wantLog != "" {
				assert.Contains(t, output, tt.wantLog)
				// For non-ErrTxDone case, also verify the injected error string.
				assert.Contains(t, output, tt.rollbackErr.Error())
			} else {
				assert.NotContains(t, output, "failed to rollback")
			}
		})
	}
}
