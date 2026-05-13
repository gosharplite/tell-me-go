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
