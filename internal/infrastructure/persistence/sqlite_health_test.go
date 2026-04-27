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
