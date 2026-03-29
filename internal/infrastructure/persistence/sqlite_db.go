// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	_ "modernc.org/sqlite"
)

// initSQLiteDB opens the SQLite database and runs migrations.
func initSQLiteDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Apply pragmas for resilience
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			return nil, fmt.Errorf("failed to set pragma %q: %w", p, err)
		}
	}

	// Run schema migrations
	err = createTables(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

func createTables(ctx context.Context, db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY,
			content TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
	}

	for _, query := range queries {
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("executing schema query: %w", err)
		}
	}
	return nil
}

// migrateFromJSON migrates data from the old JSON/MD files into the new SQLite DB if the DB is empty.
func migrateFromJSON(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath string) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
		return fmt.Errorf("checking tasks table: %w", err)
	}
	if count > 0 {
		return nil // Already migrated or populated
	}

	if err := migrateTasks(ctx, db, fs, tasksPath); err != nil {
		log.Printf("Failed to migrate tasks: %v", err)
	}

	return nil
}

func migrateTasks(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath string) error {
	stat, err := fs.Stat(ctx, tasksPath)
	if err != nil || stat == nil || stat.IsDir() {
		return nil // Nothing to migrate, or not a file
	}

	oldTasks := newTaskRepository(fs, tasksPath)
	tasks, err := oldTasks.ReadAll(ctx)
	if err != nil {
		return fmt.Errorf("reading legacy tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting tasks migration transaction: %w", err)
	}
	// Defer rollback to ensure it's called on error
	defer func() {
		_ = tx.Rollback()
	}()

	for _, t := range tasks {
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
			int64(t.ID), t.Content, t.Status, t.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("inserting legacy task %d: %w", int(t.ID), err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing tasks migration: %w", err)
	}
	return nil
}

// GetRetentionDays reads the backup retention setting directly from the database file.
// This allows the system to discover its policy before a full session initialization.
func GetRetentionDays(dbPath string) int {
	const defaultDays = 30

	// Check if file exists to avoid creating an empty database file during the probe.
	if _, err := os.Stat(dbPath); err != nil {
		return defaultDays
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return defaultDays
	}
	defer db.Close()

	// Apply busy_timeout to prevent the probe from failing during concurrent sessions or migrations.
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")

	var value string
	err = db.QueryRow("SELECT value FROM settings WHERE key = 'backup_retention_days'").Scan(&value)
	if err != nil {
		return defaultDays
	}

	var days int
	if _, err := fmt.Sscanf(value, "%d", &days); err != nil {
		return defaultDays
	}

	return days
}
