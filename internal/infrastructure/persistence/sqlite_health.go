// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// SQLiteHealthChecker implements ports.HealthChecker for SQLite database.
type SQLiteHealthChecker struct {
	db     *sql.DB
	dbPath string
}

// NewSQLiteHealthChecker creates a new SQLiteHealthChecker.
func NewSQLiteHealthChecker(db *sql.DB, dbPath string) *SQLiteHealthChecker {
	return &SQLiteHealthChecker{
		db:     db,
		dbPath: dbPath,
	}
}

// Check performs a diagnostic check on the SQLite database.
func (c *SQLiteHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	details := make(map[string]any)
	details["path"] = c.dbPath

	report := &ports.ComponentReport{
		Component: ports.CompPersistence,
		Status:    ports.StatusHealthy,
		Message:   "SQLite database is healthy",
		Details:   details,
	}

	// Step A: Filesystem Check
	dir := filepath.Dir(c.dbPath)
	if _, err := os.Stat(dir); err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database directory does not exist: %v", err)
		report.Error = err
		return report, nil
	}

	// Simple writability check for the directory
	tempFile := filepath.Join(dir, ".health_check_tmp")
	if f, err := os.Create(tempFile); err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database directory is not writable: %v", err)
		report.Error = err
		return report, nil
	} else {
		_ = f.Close()
		_ = os.Remove(tempFile)
	}

	// Get file size
	if dbInfo, err := os.Stat(c.dbPath); err == nil {
		details["size_bytes"] = dbInfo.Size()
	} else {
		details["size_bytes"] = 0
	}

	// Step B: Connection Check
	if err := c.db.PingContext(ctx); err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database connection failed: %v", err)
		report.Error = err
		return report, nil
	}

	// Step C: Integrity Check
	var integrityResult string
	err := c.db.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrityResult)
	if err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("integrity check failed to execute: %v", err)
		report.Error = err
		return report, nil
	}
	details["integrity_result"] = integrityResult
	if integrityResult != "ok" {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database corruption detected: %s", integrityResult)
		return report, nil
	}

	// Step D: Read-Only Check
	var queryOnly int
	err = c.db.QueryRowContext(ctx, "PRAGMA query_only;").Scan(&queryOnly)
	if err == nil && queryOnly == 1 {
		report.Status = ports.StatusDegraded
		report.Message = "database is in read-only mode"
	}

	return report, nil
}

// noOpHealthChecker is a fallback for when the storage is not SQLite.
type noOpHealthChecker struct {
	comp ports.Component
}

func (h *noOpHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	return &ports.ComponentReport{
		Component: h.comp,
		Status:    ports.StatusHealthy,
		Message:   "Health check not implemented for this storage type",
	}, nil
}
