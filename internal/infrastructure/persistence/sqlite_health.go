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

// sqliteHealthChecker implements ports.HealthChecker for SQLite database.
type sqliteHealthChecker struct {
	db     *sql.DB
	dbPath string
}

// newSQLiteHealthChecker creates a new sqliteHealthChecker.
func newSQLiteHealthChecker(db *sql.DB, dbPath string) *sqliteHealthChecker {
	return &sqliteHealthChecker{
		db:     db,
		dbPath: dbPath,
	}
}

// Check performs a diagnostic check on the SQLite database.
func (c *sqliteHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	details := make(map[string]any)
	details["path"] = c.dbPath

	report := &ports.ComponentReport{
		Component: ports.CompPersistence,
		Status:    ports.StatusHealthy,
		Message:   "SQLite database is healthy",
		Details:   details,
	}

	if c.checkFilesystem(ctx, report, details) {
		return report, nil
	}
	if c.checkConnection(ctx, report) {
		return report, nil
	}
	if c.checkIntegrity(ctx, report, details) {
		return report, nil
	}
	c.checkReadOnly(ctx, report)

	return report, nil
}

func (c *sqliteHealthChecker) checkFilesystem(ctx context.Context, report *ports.ComponentReport, details map[string]any) bool {
	_ = ctx

	dir := filepath.Dir(c.dbPath)
	if _, err := os.Stat(dir); err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database directory does not exist: %v", err)
		report.Error = err
		return true
	}

	// Simple writability check for the directory
	f, err := os.CreateTemp(dir, "health_check_*.tmp")
	if err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database directory is not writable: %v", err)
		report.Error = err
		return true
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()

	// Get file size
	if dbInfo, err := os.Stat(c.dbPath); err == nil {
		details["size_bytes"] = dbInfo.Size()
	} else {
		details["size_bytes"] = 0
	}

	return false
}

func (c *sqliteHealthChecker) checkConnection(ctx context.Context, report *ports.ComponentReport) bool {
	if err := c.db.PingContext(ctx); err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database connection failed: %v", err)
		report.Error = err
		return true
	}
	return false
}

func (c *sqliteHealthChecker) checkIntegrity(ctx context.Context, report *ports.ComponentReport, details map[string]any) bool {
	var integrityResult string
	err := c.db.QueryRowContext(ctx, "PRAGMA integrity_check;").Scan(&integrityResult)
	if err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("integrity check failed to execute: %v", err)
		report.Error = err
		return true
	}
	details["integrity_result"] = integrityResult
	if integrityResult != "ok" {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("database corruption detected: %s", integrityResult)
		return true
	}
	return false
}

func (c *sqliteHealthChecker) checkReadOnly(ctx context.Context, report *ports.ComponentReport) bool {
	var queryOnly int
	err := c.db.QueryRowContext(ctx, "PRAGMA query_only;").Scan(&queryOnly)
	if err == nil && queryOnly == 1 {
		report.Status = ports.StatusDegraded
		report.Message = "database is in read-only mode"
	}
	return false
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
