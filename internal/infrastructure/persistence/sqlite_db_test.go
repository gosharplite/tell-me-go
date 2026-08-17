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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
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

// TestInitSQLiteDB_Pragmas is the regression test for the DSN pragma fix
// (issue #1383): the _pragma= DSN parameters must be applied to every
// connection. This test fails against the old inert DSN
// (?_journal_mode=WAL&_busy_timeout=5000), which modernc.org/sqlite strips.
func TestInitSQLiteDB_Pragmas(t *testing.T) {
	t.Parallel()

	db, err := initSQLiteDB(context.Background(), filepath.Join(t.TempDir(), "pragmas.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var journalMode string
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode))
	assert.Equal(t, "wal", journalMode)

	var busyTimeout int
	require.NoError(t, db.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout))
	assert.GreaterOrEqual(t, busyTimeout, 5000)
}

// TestIsBusyErr_RealBusy verifies the true branch of isBusyErr with a genuine
// driver SQLITE_BUSY error — no mocks, no string fixtures. A raw driver
// connection holds the RESERVED lock (BEGIN IMMEDIATE) while a database/sql
// handle with busy_timeout 0 (bare-path DSN) attempts a write; the driver
// returns a real *sqlite.Error with result code 5.
func TestIsBusyErr_RealBusy(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "busy.db")

	// Lock holder: raw driver connection (bare path, busy_timeout 0).
	lockConn, err := sqliteDriver.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lockConn.Close() })

	// BEGIN IMMEDIATE acquires the RESERVED lock immediately. (A driver.Tx
	// from Begin() is deferred and takes no lock — ExecContext is required.)
	_, err = lockConn.(driver.ExecerContext).ExecContext(context.Background(), "BEGIN IMMEDIATE", nil)
	require.NoError(t, err)

	// SUT connection: bare path → busy_timeout 0 → immediate SQLITE_BUSY.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "CREATE TABLE t (id INTEGER)")
	require.Error(t, err)
	assert.True(t, isBusyErr(err), "expected SQLITE_BUSY, got: %v", err)
}

// TestIsBusyErr_NonBusy verifies the false branch of isBusyErr with two
// genuine non-busy driver errors: a READONLY (code 8) error and a plain
// closed-DB error.
func TestIsBusyErr_NonBusy(t *testing.T) {
	t.Parallel()

	t.Run("readonly code 8", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ro.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		_, err = db.Exec("PRAGMA query_only = 1")
		require.NoError(t, err)

		_, err = db.ExecContext(context.Background(), "CREATE TABLE t (id INTEGER)")
		require.Error(t, err)
		assert.False(t, isBusyErr(err))
		// Sanity: it really is a READONLY (code 8) error, not something else.
		var se *sqlite.Error
		if errors.As(err, &se) {
			assert.Equal(t, 8, se.Code())
		}
	})

	t.Run("closed db", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "closed.db"))
		require.NoError(t, err)
		_ = db.Close() // closed before any query → non-busy driver error

		_, err = db.ExecContext(context.Background(), "CREATE TABLE t (id INTEGER)")
		require.Error(t, err)
		assert.False(t, isBusyErr(err))
	})
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

// =============================================================================
// Task 3 (issue #1383): withBusyRetry — bounded, ctx-honoring, fail-fast
// backoff for SQLITE_BUSY. Both tests are fully deterministic per ADR-036:
// no time.Sleep, no timing assertions. Cancellation and fail-fast are proven
// structurally (by ctx state and invocation count), never by waiting.
// =============================================================================

// TestWithBusyRetry_NonBusyFailsFast verifies that a non-busy error (READONLY,
// code 8) fails immediately: fn is invoked exactly once and the error chain is
// returned unchanged (code 8 detectable via errors.As).
func TestWithBusyRetry_NonBusyFailsFast(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ff.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA query_only = 1")
	require.NoError(t, err)

	var calls atomic.Int32
	err = withBusyRetry(context.Background(), func() error {
		calls.Add(1)
		_, e := db.ExecContext(context.Background(), "CREATE TABLE t (id INTEGER)")
		return e
	}, 3)

	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "non-busy error must fail fast: fn invoked exactly once")
	var se *sqlite.Error
	if errors.As(err, &se) {
		assert.Equal(t, 8, se.Code(), "error chain must be unchanged (READONLY)")
	}
}

// TestCreateTables_BusyCancellation verifies that cancellation during backoff
// surfaces as ctx.Err() and fn is never retried. Deterministic by ctx state:
// the fn itself cancels the ctx on the first (busy) invocation, so
// withBusyRetry's backoff select sees an already-closed ctx.Done().
func TestCreateTables_BusyCancellation(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "cancel.db")

	// Lock holder (bare path → busy_timeout 0 → immediate genuine busy).
	lockConn, err := sqliteDriver.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lockConn.Close() })
	_, err = lockConn.(driver.ExecerContext).ExecContext(context.Background(), "BEGIN IMMEDIATE", nil)
	require.NoError(t, err)

	// SUT connection: bare path, no busy_timeout.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	err = withBusyRetry(ctx, func() error {
		if calls.Add(1) == 1 {
			cancel() // cancel during backoff: deterministic by ctx state
		}
		return createTables(ctx, db)
	}, 3)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "cancellation must surface as ctx.Err(), got: %v", err)
	assert.Equal(t, int32(1), calls.Load(), "fn must not be retried after cancellation")
}

// =============================================================================
// Task 5 (issue #1383): deterministic createTables SQLITE_BUSY retry tests
// with a real-busy lock harness. The release→ack handshake (ROLLBACK's
// ExecContext return is the ack) sequences the release before the next
// attempt with no timing window — zero time.Sleep, zero timing assertions,
// zero require.Eventually (ADR-036 determinism).
// =============================================================================

// holdBusyLock acquires a real SQLITE_BUSY lock on dbPath via a raw driver
// connection (bare path → busy_timeout 0 → contended statements fail
// immediately with a genuine *sqlite.Error{code:5}). The returned release
// func executes ROLLBACK synchronously: the lock is guaranteed released when
// release returns (ExecContext's return is the ack), so a caller can sequence
// the release before withBusyRetry's next attempt with no timing window.
func holdBusyLock(t *testing.T, dbPath string) (release func()) {
	t.Helper()

	conn, err := sqliteDriver.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ec, ok := conn.(driver.ExecerContext)
	require.True(t, ok, "raw sqlite conn must implement driver.ExecerContext")
	if _, err := ec.ExecContext(context.Background(), "BEGIN IMMEDIATE", nil); err != nil {
		t.Fatalf("BEGIN IMMEDIATE failed: %v", err)
	}

	return func() {
		if _, err := ec.ExecContext(context.Background(), "ROLLBACK", nil); err != nil {
			t.Fatalf("release (ROLLBACK) failed: %v", err)
		}
	}
}

// TestCreateTables_RetriesOnRealBusy proves the retry behavior end-to-end at
// the withBusyRetry + createTables level: attempt 1 hits a genuine SQLITE_BUSY
// (real RESERVED lock held by holdBusyLock), the release→ack handshake fires
// INSIDE the retried fn, and attempt 2 succeeds. Attempt counter == 2 proves
// the retry fired exactly once.
func TestCreateTables_RetriesOnRealBusy(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "retry.db")
	release := holdBusyLock(t, dbPath)

	// SUT connection: bare path → busy_timeout 0 → immediate genuine busy.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var attempts atomic.Int32
	err = withBusyRetry(context.Background(), func() error {
		n := attempts.Add(1)
		err := createTables(context.Background(), db)
		// Release→ack handshake INSIDE the retried fn: the lock is released
		// (and the release acked by ExecContext's return) before the busy
		// error is handed to withBusyRetry, so the 50 ms backoff can never
		// race the release.
		if n == 1 && isBusyErr(err) {
			release()
		}
		return err
	}, createTablesAttempts)

	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load(), "transient busy must succeed on attempt 2")

	// Tables exist.
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM tasks").Scan(&n))
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM settings").Scan(&n))
}

// TestCreateTables_PersistentBusyBounded proves the retry budget is honored
// with a persistent (never-released) lock: exactly createTablesAttempts (2)
// invocations, and the returned error is the last real busy error (code 5 via
// errors.As, wrap string intact).
func TestCreateTables_PersistentBusyBounded(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "persist.db")
	_ = holdBusyLock(t, dbPath) // never released

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var attempts atomic.Int32
	err = withBusyRetry(context.Background(), func() error {
		attempts.Add(1)
		return createTables(context.Background(), db)
	}, createTablesAttempts)

	require.Error(t, err)
	assert.Equal(t, int32(2), attempts.Load(), "persistent busy must exhaust exactly createTablesAttempts (2)")

	var se *sqlite.Error
	require.True(t, errors.As(err, &se), "exhaustion must return the real busy error, got: %v", err)
	assert.Equal(t, sqliteBusyCode, se.Code())
	assert.Contains(t, err.Error(), "executing schema query")
}

// =============================================================================
// Task 6 (issue #1383): deterministic migrateFromJSON SQLITE_BUSY retry +
// convergence tests. Real-busy via the T5 holdBusyLock harness (RESERVED lock
// from BEGIN IMMEDIATE), release→ack handshake inside the retried fn —
// zero time.Sleep, zero timing assertions (ADR-036). queryLoggingConnector
// pins the exact query sequence for the winner-commits convergence proof.
// =============================================================================

// writeNTasks writes a valid N-task JSON file (IDs 1..N, distinct content,
// RFC3339 created_at) via fs — the shared seeder for the migrate busy tests.
func writeNTasks(t *testing.T, ctx context.Context, fs persistence.FileSystem, tasksPath string, n int) {
	t.Helper()

	tasksJSON := "["
	for i := 1; i <= n; i++ {
		if i > 1 {
			tasksJSON += ","
		}
		tasksJSON += fmt.Sprintf(`{"id": %d, "content": "Migrated Task %d", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}`, i, i)
	}
	tasksJSON += "]"

	if err := fs.WriteFile(ctx, tasksPath, []byte(tasksJSON), 0644); err != nil {
		t.Fatal(err)
	}
}

// queryLoggingConnector wraps a raw sqlite connection and appends every
// ExecContext/QueryContext SQL string to a mutex-guarded log, so tests can
// pin the exact query sequence (ADR-036 race-safety: database/sql pool
// goroutines append concurrently).
type queryLoggingConnector struct {
	dbPath string
	log    *[]string
	logMu  *sync.Mutex
}

func (c *queryLoggingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	realConn, err := sqliteDriver.Open(c.dbPath)
	if err != nil {
		return nil, err
	}
	return &queryLoggingConn{conn: realConn, log: c.log, logMu: c.logMu}, nil
}

func (c *queryLoggingConnector) Driver() driver.Driver { return sqliteDriver }

type queryLoggingConn struct {
	conn  driver.Conn
	log   *[]string
	logMu *sync.Mutex
}

func (c *queryLoggingConn) logQuery(query string) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	*c.log = append(*c.log, query)
}

func (c *queryLoggingConn) Prepare(query string) (driver.Stmt, error) { return c.conn.Prepare(query) }
func (c *queryLoggingConn) Close() error                              { return c.conn.Close() }
func (c *queryLoggingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *queryLoggingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if bc, ok := c.conn.(driver.ConnBeginTx); ok {
		return bc.BeginTx(ctx, opts)
	}
	return c.conn.Begin() //nolint:staticcheck // SA1019: fallback for older drivers
}

func (c *queryLoggingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.logQuery(query)
	if qc, ok := c.conn.(driver.QueryerContext); ok {
		return qc.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *queryLoggingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.logQuery(query)
	if ec, ok := c.conn.(driver.ExecerContext); ok {
		return ec.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

// TestMigrateFromJSON_RetriesOnRealBusy proves migrateFromJSON retries on a
// genuine SQLITE_BUSY at the batch INSERT (the COUNT read passes the RESERVED
// lock; only the write contends). Attempt 1 releases the lock inside the
// retried fn (T5 release→ack pattern); attempt 2 migrates fully; final
// COUNT == N proves single-transaction atomicity + INSERT OR REPLACE
// idempotency made the retry safe.
func TestMigrateFromJSON_RetriesOnRealBusy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.json")
	dbPath := filepath.Join(dir, "test.db")
	fs := NewOSFileSystem()
	writeNTasks(t, ctx, fs, tasksPath, 5)

	// SUT connection: bare path → busy_timeout 0 → immediate genuine busy.
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// Create the tasks table BEFORE taking the lock: a held RESERVED lock
	// busy-fails the CREATE TABLE (write), which would break the premise
	// that only the batch INSERT contends.
	require.NoError(t, createTables(ctx, db))

	release := holdBusyLock(t, dbPath)

	var attempts atomic.Int32
	err = withBusyRetry(ctx, func() error {
		n := attempts.Add(1)
		err := migrateFromJSON(ctx, db, fs, tasksPath, slog.Default())
		if n == 1 && isBusyErr(err) {
			release() // release→ack inside the retried fn (T5 pattern)
		}
		return err
	}, migrateAttempts)

	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load(), "transient busy must succeed on attempt 2")

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count))
	assert.Equal(t, 5, count)
}

// TestMigrateFromJSON_PersistentBusyBounded proves the retry budget is
// honored with a persistent (never-released) lock: exactly migrateAttempts
// (3) invocations, and the exhaustion error is the last real busy error
// (code 5 via errors.As) wrapped through both migrateFromJSON layers.
func TestMigrateFromJSON_PersistentBusyBounded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.json")
	dbPath := filepath.Join(dir, "test.db")
	fs := NewOSFileSystem()
	writeNTasks(t, ctx, fs, tasksPath, 5)

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, createTables(ctx, db))

	_ = holdBusyLock(t, dbPath) // never released

	var attempts atomic.Int32
	err = withBusyRetry(ctx, func() error {
		attempts.Add(1)
		return migrateFromJSON(ctx, db, fs, tasksPath, slog.Default())
	}, migrateAttempts)

	require.Error(t, err)
	assert.Equal(t, int32(3), attempts.Load(), "persistent busy must exhaust exactly migrateAttempts (3)")

	var se *sqlite.Error
	require.True(t, errors.As(err, &se), "exhaustion must return the real busy error, got: %v", err)
	assert.Equal(t, sqliteBusyCode, se.Code())
	assert.Contains(t, err.Error(), "migrating legacy tasks from")
	assert.Contains(t, err.Error(), "bulk inserting legacy tasks")
}

// TestMigrateFromJSON_WinnerCommitsBetweenAttempts pins the convergence
// behavior: B's attempt 1 hits genuine busy at the INSERT; the lock is
// released and a third bare-path connection C migrates + commits all N rows
// while B is between attempts (migrateFromJSON returns only after COMMIT, so
// attempt 2 deterministically sees COUNT == N). Attempt 2 takes the
// fast-path skip (count > 0 → nil), so the query log is exactly
// [COUNT, INSERT(busy)] → [COUNT] with zero ExecContexts on attempt 2.
func TestMigrateFromJSON_WinnerCommitsBetweenAttempts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	tasksPath := filepath.Join(dir, "tasks.json")
	dbPath := filepath.Join(dir, "test.db")
	fs := NewOSFileSystem()
	writeNTasks(t, ctx, fs, tasksPath, 5)

	var log []string
	var logMu sync.Mutex
	db := sql.OpenDB(&queryLoggingConnector{dbPath: dbPath, log: &log, logMu: &logMu})
	t.Cleanup(func() { _ = db.Close() })

	// Create the tasks table on the SUT connection BEFORE taking the lock
	// (a held RESERVED lock would busy-fail the CREATE TABLE). The two
	// CREATE TABLE ExecContexts land in the query log, so reset the log
	// before the migrate attempts — the pin below must see exactly
	// [COUNT, INSERT(busy)] → [COUNT].
	require.NoError(t, createTables(ctx, db))
	logMu.Lock()
	log = log[:0]
	logMu.Unlock()

	release := holdBusyLock(t, dbPath)

	var attempts atomic.Int32
	err := withBusyRetry(ctx, func() error {
		n := attempts.Add(1)
		err := migrateFromJSON(ctx, db, fs, tasksPath, slog.Default())
		if n == 1 && isBusyErr(err) {
			release()
			// Winner: a third bare-path connection migrates and commits N rows
			// while B is between attempts; migrateFromJSON returns only after
			// COMMIT completes, so attempt 2 deterministically sees COUNT == N.
			dbC, cerr := sql.Open("sqlite", dbPath)
			require.NoError(t, cerr)
			cerr = migrateFromJSON(ctx, dbC, fs, tasksPath, slog.Default())
			require.NoError(t, cerr)
			require.NoError(t, dbC.Close())
		}
		return err
	}, migrateAttempts)

	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load())

	// Query-log pin: [COUNT, INSERT(busy)] -> [COUNT]; zero INSERTs on attempt 2.
	// NOTE: explicit Unlock (not defer) — the convergence COUNT below
	// re-enters the logging connector and would self-deadlock on a held logMu.
	logMu.Lock()
	require.Len(t, log, 3, "log = %v", log)
	assert.Equal(t, "SELECT COUNT(*) FROM tasks", log[0])
	assert.Contains(t, log[1], "INSERT OR REPLACE INTO tasks")
	assert.Equal(t, "SELECT COUNT(*) FROM tasks", log[2])
	logMu.Unlock()

	// Convergence: exactly N rows, committed by the winner, observed by B's skip.
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count))
	assert.Equal(t, 5, count)
}
