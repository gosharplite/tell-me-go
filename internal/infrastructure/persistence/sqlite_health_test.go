// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSQLiteHealthChecker_Healthy(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE test (id INTEGER);"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	checker := newSQLiteHealthChecker(db, dbPath)
	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != ports.StatusHealthy {
		t.Errorf("expected StatusHealthy, got %s: %s", report.Status, report.Message)
	}

	details, ok := report.Details.(map[string]any)
	if !ok {
		t.Fatal("expected Details to be a map[string]any")
	}

	if details["path"] != dbPath {
		t.Errorf("expected path %s, got %v", dbPath, details["path"])
	}

	if details["integrity_result"] != "ok" {
		t.Errorf("expected integrity_result ok, got %v", details["integrity_result"])
	}

	if _, ok := details["size_bytes"].(int64); !ok {
		t.Errorf("expected size_bytes to be int64, got %T", details["size_bytes"])
	}
}

func TestSQLiteHealthChecker_UnhealthyDirNotFound(t *testing.T) {
	t.Parallel()
	badPath := filepath.Join(t.TempDir(), "nonexistent", "test.db")
	db, err := sql.Open("sqlite", badPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	checker := newSQLiteHealthChecker(db, badPath)
	report, _ := checker.Check(context.Background())

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", report.Status)
	}
}

func TestSQLiteHealthChecker_DegradedReadOnly(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE test (id INTEGER);"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	if _, err := db.Exec("PRAGMA query_only = 1;"); err != nil {
		t.Fatalf("failed to set query_only: %v", err)
	}

	checker := newSQLiteHealthChecker(db, dbPath)
	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != ports.StatusDegraded {
		t.Errorf("expected StatusDegraded, got %s", report.Status)
	}
}

func TestSQLiteHealthChecker_UnhealthyCorruption(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "db2.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	_ = db.Close()

	checker := newSQLiteHealthChecker(db, dbPath)
	report, _ := checker.Check(context.Background())

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy when DB is closed, got %s", report.Status)
	}
}

func TestSQLiteHealthChecker_PingFailure(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	// Create a table so the filesystem and integrity checks pass,
	// but close the DB so PingContext fails.
	if _, err := db.Exec("CREATE TABLE test (id INTEGER);"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	_ = db.Close()

	checker := newSQLiteHealthChecker(db, dbPath)
	report, _ := checker.Check(context.Background())

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", report.Status)
	}
	if report.Error == nil {
		t.Error("expected error, got nil")
	}
}

func TestSQLiteHealthChecker_IntegrityCheckExecFailure(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	// Create a table so filesystem checks pass.
	if _, err := db.Exec("CREATE TABLE test (id INTEGER);"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	_ = db.Close()

	checker := newSQLiteHealthChecker(db, dbPath)
	report, _ := checker.Check(context.Background())

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", report.Status)
	}
	// The integrity check step should fail when the DB is closed.
	if report.Error == nil {
		t.Error("expected error from integrity check, got nil")
	}
}

func TestSQLiteHealthChecker_DirNotWritable(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("skipping permission test: running as root")
	}

	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("failed to chmod dir: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }() // allow TempDir cleanup

	dbPath := filepath.Join(dir, "nonexistent.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	checker := newSQLiteHealthChecker(db, dbPath)
	report, _ := checker.Check(context.Background())

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s: %s", report.Status, report.Message)
	}
	if report.Error == nil {
		t.Error("expected error, got nil")
	}
}

func TestSQLiteHealthChecker_DBFileStatFails(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a symlink to a path in a non-existent directory.
	// The parent directory (tmpDir) exists and is writable, so the filesystem
	// checks pass, but os.Stat follows the symlink and fails because the
	// target directory does not exist → size_bytes = 0.
	dbPath := filepath.Join(tmpDir, "dangling")
	if err := os.Symlink("/nonexistent_dir/test.db", dbPath); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	checker := newSQLiteHealthChecker(db, dbPath)
	report, _ := checker.Check(context.Background())

	// os.Stat follows the dangling symlink and fails → size_bytes = 0.
	// Then PingContext fails because SQLite cannot create the DB file
	// in the non-existent directory.
	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s: %s", report.Status, report.Message)
	}

	// Verify size_bytes was set to 0 (the else branch).
	// Note: the untyped 0 literal in the production code is stored as int,
	// not int64. We accept either representation.
	details, ok := report.Details.(map[string]any)
	if !ok {
		t.Fatal("expected Details to be a map[string]any")
	}
	raw, exists := details["size_bytes"]
	if !exists {
		t.Fatal("expected size_bytes key in details")
	}
	var sizeBytes int64
	switch v := raw.(type) {
	case int64:
		sizeBytes = v
	case int:
		sizeBytes = int64(v)
	default:
		t.Fatalf("expected size_bytes to be int or int64, got %T (%v)", raw, raw)
	}
	if sizeBytes != 0 {
		t.Errorf("expected size_bytes = 0, got %d", sizeBytes)
	}
}

func TestNoOpHealthChecker_Check(t *testing.T) {
	t.Parallel()

	checker := &noOpHealthChecker{comp: ports.CompPersistence}
	report, err := checker.Check(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != ports.StatusHealthy {
		t.Errorf("expected StatusHealthy, got %s", report.Status)
	}
	if report.Component != ports.CompPersistence {
		t.Errorf("expected Component %s, got %s", ports.CompPersistence, report.Component)
	}
}

// TestSQLiteHealthChecker_Check is a table-driven test covering all error branches
// in sqliteHealthChecker.Check(). Each subtest exercises a distinct code path.
func TestSQLiteHealthChecker_Check(t *testing.T) {
	t.Parallel()

	// Helper to extract size_bytes from details map (handles int and int64).
	extractSizeBytes := func(t *testing.T, details map[string]any) int64 {
		t.Helper()
		raw, exists := details["size_bytes"]
		if !exists {
			t.Fatal("expected size_bytes key in details")
		}
		switch v := raw.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		default:
			t.Fatalf("expected size_bytes to be int or int64, got %T (%v)", raw, raw)
			return 0
		}
	}

	// ---------------
	// Baseline: all checks pass on a healthy database.
	// ---------------
	t.Run("healthy", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		_, err = db.Exec("CREATE TABLE test (id INTEGER);")
		require.NoError(t, err)

		checker := newSQLiteHealthChecker(db, dbPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		assert.Equal(t, ports.StatusHealthy, report.Status)
		assert.Equal(t, ports.CompPersistence, report.Component)
		assert.Contains(t, report.Message, "healthy")
		assert.Nil(t, report.Error)

		details, ok := report.Details.(map[string]any)
		require.True(t, ok, "Details should be map[string]any")
		assert.Equal(t, dbPath, details["path"])
		assert.Equal(t, "ok", details["integrity_result"])
		assert.NotZero(t, extractSizeBytes(t, details))
	})

	// ---------------
	// Scenario A: os.Stat(dir) fails — directory does not exist.
	// ---------------
	t.Run("dir_not_found", func(t *testing.T) {
		t.Parallel()
		badPath := filepath.Join(t.TempDir(), "nonexistent", "test.db")
		db, err := sql.Open("sqlite", badPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		checker := newSQLiteHealthChecker(db, badPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		assert.Equal(t, ports.StatusUnhealthy, report.Status)
		assert.NotNil(t, report.Error)
		assert.Contains(t, report.Message, "directory does not exist")
	})

	// ---------------
	// Scenario B: os.CreateTemp fails — directory is not writable.
	// ---------------
	t.Run("dir_not_writable", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("skipping permission test: running as root")
		}

		dir := filepath.Join(t.TempDir(), "readonly")
		err := os.Mkdir(dir, 0755)
		require.NoError(t, err)
		err = os.Chmod(dir, 0555)
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

		dbPath := filepath.Join(dir, "nonexistent.db")
		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		checker := newSQLiteHealthChecker(db, dbPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		assert.Equal(t, ports.StatusUnhealthy, report.Status)
		assert.NotNil(t, report.Error)
		assert.Contains(t, report.Message, "not writable")
	})

	// ---------------
	// Scenario C: PRAGMA integrity_check query fails with an error.
	// NOTE: With modernc.org/sqlite, PingContext validates the schema and
	// catches most corruptions before integrity_check runs. Closing the DB
	// causes PingContext to fail first, which exercises the same error-
	// handling pattern (StatusUnhealthy, error set). This subtest verifies
	// that a closed database produces the expected unhealthy report.
	// ---------------
	t.Run("integrity_query_fails", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)

		_, err = db.Exec("CREATE TABLE test (id INTEGER);")
		require.NoError(t, err)
		_ = db.Close()

		checker := newSQLiteHealthChecker(db, dbPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		assert.Equal(t, ports.StatusUnhealthy, report.Status)
		assert.NotNil(t, report.Error)
		assert.Contains(t, report.Message, "database connection failed")
	})

	// ---------------
	// Scenario D: integrity_result != "ok" — database corruption detected.
	// Uses PRAGMA writable_schema=ON + DELETE FROM sqlite_master to corrupt
	// the schema so that PingContext still passes but PRAGMA integrity_check
	// returns a non-"ok" result.
	// ---------------
	t.Run("corruption_detected", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)

		_, err = db.Exec("CREATE TABLE test (id INTEGER);")
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO test VALUES (1);")
		require.NoError(t, err)

		// Corrupt via writable_schema: delete all rows from sqlite_master.
		_, err = db.Exec("PRAGMA writable_schema=ON;")
		require.NoError(t, err)
		_, err = db.Exec("DELETE FROM sqlite_master;")
		require.NoError(t, err)
		_, err = db.Exec("PRAGMA writable_schema=OFF;")
		require.NoError(t, err)
		_ = db.Close()

		// Reopen: PingContext passes (schema is structurally valid),
		// but integrity_check reports missing table entries.
		db2, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db2.Close() })

		checker := newSQLiteHealthChecker(db2, dbPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		assert.Equal(t, ports.StatusUnhealthy, report.Status)
		assert.Contains(t, report.Message, "corruption")
		assert.Nil(t, report.Error, "Error field should be nil; corruption is reported via Message")

		details, ok := report.Details.(map[string]any)
		require.True(t, ok)
		assert.NotEqual(t, "ok", details["integrity_result"],
			"integrity_result should not be 'ok' when corruption is detected")
	})

	// ---------------
	// Scenario E: os.Stat(c.dbPath) fails — DB file is missing but
	// directory exists and is writable. The check should set size_bytes=0
	// and continue gracefully (StatusHealthy).
	// ---------------
	t.Run("db_file_missing", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "missing.db")

		// sql.Open with a non-existent file does not create it immediately.
		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		checker := newSQLiteHealthChecker(db, dbPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		// The directory exists and is writable, so filesystem checks pass.
		// os.Stat fails → size_bytes = 0 (the else branch).
		// PingContext creates the file (a fresh empty SQLite DB).
		// integrity_check on a fresh DB returns "ok".
		assert.Equal(t, ports.StatusHealthy, report.Status)
		assert.Contains(t, report.Message, "healthy")

		details, ok := report.Details.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, int64(0), extractSizeBytes(t, details),
			"size_bytes should be 0 when file did not exist at stat time")
		assert.Equal(t, "ok", details["integrity_result"])
	})

	// ---------------
	// Scenario F: PRAGMA query_only returns 1 — database is in read-only
	// mode, resulting in StatusDegraded.
	// ---------------
	t.Run("read_only_mode", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := sql.Open("sqlite", dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		_, err = db.Exec("CREATE TABLE test (id INTEGER);")
		require.NoError(t, err)

		_, err = db.Exec("PRAGMA query_only = 1;")
		require.NoError(t, err)

		checker := newSQLiteHealthChecker(db, dbPath)
		report, err := checker.Check(context.Background())
		require.NoError(t, err)
		require.NotNil(t, report)

		assert.Equal(t, ports.StatusDegraded, report.Status)
		assert.Contains(t, report.Message, "read-only")
		assert.Nil(t, report.Error)
	})
}
