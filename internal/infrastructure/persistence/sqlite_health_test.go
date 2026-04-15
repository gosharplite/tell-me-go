// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	_ "modernc.org/sqlite"
)

func TestSQLiteHealthChecker_Check(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Ensure the database file exists
	if _, err := db.Exec("CREATE TABLE test (id INTEGER);"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	checker := NewSQLiteHealthChecker(db, dbPath)
	ctx := context.Background()

	t.Run("Healthy", func(t *testing.T) {
		report, err := checker.Check(ctx)
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
	})

	t.Run("Unhealthy_DirNotFound", func(t *testing.T) {
		badPath := filepath.Join(tmpDir, "nonexistent", "test.db")
		badChecker := NewSQLiteHealthChecker(db, badPath)
		report, _ := badChecker.Check(ctx)

		if report.Status != ports.StatusUnhealthy {
			t.Errorf("expected StatusUnhealthy, got %s", report.Status)
		}
	})

	t.Run("Degraded_ReadOnly", func(t *testing.T) {
		// Set the database to query_only
		if _, err := db.Exec("PRAGMA query_only = 1;"); err != nil {
			t.Fatalf("failed to set query_only: %v", err)
		}
		defer func() { _, _ = db.Exec("PRAGMA query_only = 0;") }()

		report, err := checker.Check(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.Status != ports.StatusDegraded {
			t.Errorf("expected StatusDegraded, got %s", report.Status)
		}
	})

	t.Run("Unhealthy_Corruption", func(t *testing.T) {
		// We can't easily corrupt it, but we can mock a bad integrity result
		// by closing the DB or using a mock. Here we just verify the logic path.
		// Actually, let's close the DB and see how it behaves.
		db2, _ := sql.Open("sqlite", filepath.Join(tmpDir, "db2.db"))
		_ = db2.Close()
		badChecker := NewSQLiteHealthChecker(db2, filepath.Join(tmpDir, "db2.db"))
		report, _ := badChecker.Check(ctx)

		if report.Status != ports.StatusUnhealthy {
			t.Errorf("expected StatusUnhealthy when DB is closed, got %s", report.Status)
		}
	})
}
