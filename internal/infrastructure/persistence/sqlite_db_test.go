// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
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
	configFile := filepath.Join(tempDir, "config.json")
	scratchFile := filepath.Join(tempDir, "scratchpad.md")
	dbPath := filepath.Join(tempDir, "test.db")

	seedLegacyFiles(t, ctx, fs, tasksFile, configFile, scratchFile)

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := migrateFromJSON(ctx, db, fs, tasksFile, configFile, scratchFile); err != nil {
		t.Fatalf("migrateFromJSON failed: %v", err)
	}

	t.Run("Config Migration", func(t *testing.T) { t.Parallel(); testConfigMigration(t, ctx, db) })
	t.Run("Scratchpad Migration", func(t *testing.T) { t.Parallel(); testScratchpadMigration(t, ctx, db) })
	t.Run("Tasks Migration", func(t *testing.T) { t.Parallel(); testTasksMigration(t, ctx, db) })
	t.Run("Idempotency", func(t *testing.T) {
		t.Parallel()
		testMigrationIdempotency(t, ctx, db, fs, tasksFile, configFile, scratchFile)
	})
}

func testConfigMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var configVal string
	if err := db.QueryRowContext(ctx, "SELECT value FROM config WHERE key = ?", "legacy_key").Scan(&configVal); err != nil {
		t.Errorf("Failed to read migrated config: %v", err)
	} else if configVal != "legacy_val" {
		t.Errorf("Config migration mismatch: expected 'legacy_val', got %q", configVal)
	}
}

func testScratchpadMigration(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var scratchVal string
	if err := db.QueryRowContext(ctx, "SELECT content FROM scratchpad WHERE id = 1").Scan(&scratchVal); err != nil {
		t.Errorf("Failed to read migrated scratchpad: %v", err)
	} else if scratchVal != "Migrated scratchpad content" {
		t.Errorf("Scratchpad migration mismatch: expected 'Migrated scratchpad content', got %q", scratchVal)
	}
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

func testMigrationIdempotency(t *testing.T, ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksFile, configFile, scratchFile string) {
	t.Helper()
	if err := migrateFromJSON(ctx, db, fs, tasksFile, configFile, scratchFile); err != nil {
		t.Fatalf("migrateFromJSON second run failed: %v", err)
	}
}

func seedLegacyFiles(t *testing.T, ctx context.Context, fs persistence.FileSystem, tasksFile, configFile, scratchFile string) {
	t.Helper()
	tasksJSON := `[{"id": 1, "content": "Migrated Task 1", "status": "pending", "created_at": "2025-01-01T00:00:00Z"}]`
	configJSON := `{"legacy_key": "legacy_val"}`
	scratchMD := "Migrated scratchpad content"

	if err := fs.WriteFile(ctx, tasksFile, []byte(tasksJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(ctx, configFile, []byte(configJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(ctx, scratchFile, []byte(scratchMD), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteMigrations_MissingFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fs := NewOSFileSystem()

	tempDir := t.TempDir()
	tasksFile := filepath.Join(tempDir, "missing_tasks.json")
	configFile := filepath.Join(tempDir, "missing_config.json")
	scratchFile := filepath.Join(tempDir, "missing_scratchpad.md")
	dbPath := filepath.Join(tempDir, "test2.db")

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Migration should not fail if files are missing
	if err := migrateFromJSON(ctx, db, fs, tasksFile, configFile, scratchFile); err != nil {
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

	if err := fs.WriteFile(ctx, tasksFile, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// This should log an error internally but not return err in migrateFromJSON, or log it
	if err := migrateFromJSON(ctx, db, fs, tasksFile, "", ""); err != nil {
		t.Fatalf("migrateFromJSON failed with invalid json: %v", err)
	}
}

func TestSQLiteMigrations_InvalidDBPath(t *testing.T) {
	t.Parallel()
	_, err := initSQLiteDB(context.Background(), "/invalid/path/db.sqlite")
	if err == nil {
		t.Error("expected error for invalid db path")
	}
}

func TestSQLiteMigrations_InvalidJSONConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := NewOSFileSystem()
	tempDir := t.TempDir()

	tasksFile := filepath.Join(tempDir, "tasks.json")
	configFile := filepath.Join(tempDir, "config.json")
	scratchFile := filepath.Join(tempDir, "scratchpad.md")
	dbPath := filepath.Join(tempDir, "test4.db")

	if err := fs.WriteFile(ctx, configFile, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := initSQLiteDB(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	err = migrateFromJSON(ctx, db, fs, tasksFile, configFile, scratchFile)
	if err != nil {
		t.Fatalf("migrateFromJSON failed with invalid json: %v", err)
	}
}
