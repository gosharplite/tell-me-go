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

// InitSQLiteDB opens the SQLite database and runs migrations.
func InitSQLiteDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Apply WAL pragma
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Run schema migrations
	err = createTables(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
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
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

// MigrateFromJSON migrates data from the old JSON/MD files into the new SQLite DB if the DB is empty.
func MigrateFromJSON(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath, configPath, scratchPath string) error {
	// Check if migration is needed (e.g. if the DB is empty).
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM config").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // Already migrated or populated
	}

	// Check if any old files exist to migrate
	hasTasks, _ := fs.Stat(ctx, tasksPath)
	hasConfig, _ := fs.Stat(ctx, configPath)
	hasScratch, _ := fs.Stat(ctx, scratchPath)

	if hasTasks == nil && hasConfig == nil && hasScratch == nil {
		return nil // Nothing to migrate, fresh install
	}

	log.Println("Migrating from JSON/MD files to SQLite...")

	// Initialize old repositories
	oldConfig := newConfigRepository(fs, configPath)
	oldTasks := newTaskRepository(fs, tasksPath)
	oldScratch := newScratchpadRepository(fs, scratchPath)

	// Migrate Config
	if hasConfig != nil {
		configs, err := oldConfig.GetAll(ctx)
		if err == nil && len(configs) > 0 {
			tx, _ := db.BeginTx(ctx, nil)
			for k, v := range configs {
				tx.ExecContext(ctx, "INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)", k, v)
			}
			tx.Commit()
		}
	}

	// Migrate Tasks
	if hasTasks != nil {
		tasks, err := oldTasks.ReadAll(ctx)
		if err == nil && len(tasks) > 0 {
			tx, _ := db.BeginTx(ctx, nil)
			for _, t := range tasks {
				tx.ExecContext(ctx, "INSERT OR REPLACE INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
					int64(t.ID), t.Content, t.Status, t.CreatedAt.Format(time.RFC3339Nano))
			}
			tx.Commit()
		}
	}

	// Migrate Scratchpad
	if hasScratch != nil {
		scratch, err := oldScratch.Get(ctx, "content")
		if err == nil && scratch != "" {
			db.ExecContext(ctx, "INSERT OR REPLACE INTO scratchpad (id, content) VALUES (1, ?)", scratch)
		}
	}

	return nil
}
