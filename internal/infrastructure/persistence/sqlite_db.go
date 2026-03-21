// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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

	// Apply WAL pragma
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
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
		`CREATE TABLE IF NOT EXISTS scratchpad (
			id INTEGER PRIMARY KEY,
			content TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY,
			content TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL
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
func migrateFromJSON(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath, scratchPath string) error {
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

	if err := migrateScratchpad(ctx, db, fs, scratchPath); err != nil {
		log.Printf("Failed to migrate scratchpad: %v", err)
	}

	return nil
}

func migrateTasks(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath string) error {
	if stat, _ := fs.Stat(ctx, tasksPath); stat == nil {
		return nil
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

	for _, t := range tasks {
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
			int64(t.ID), t.Content, t.Status, t.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("inserting legacy task %d: %w", int(t.ID), err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing tasks migration: %w", err)
	}
	return nil
}

func migrateScratchpad(ctx context.Context, db *sql.DB, fs persistence.FileSystem, scratchPath string) error {
	if stat, _ := fs.Stat(ctx, scratchPath); stat == nil {
		return nil
	}
	oldScratch := newScratchpadRepository(fs, scratchPath)
	scratch, err := oldScratch.Get(ctx, "content")
	if err != nil {
		return fmt.Errorf("reading legacy scratchpad: %w", err)
	}
	if scratch == "" {
		return nil
	}

	_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO scratchpad (id, content) VALUES (1, ?)", scratch)
	if err != nil {
		return fmt.Errorf("inserting legacy scratchpad: %w", err)
	}
	return nil
}
