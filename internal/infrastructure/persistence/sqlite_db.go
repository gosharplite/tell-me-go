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
func initSQLiteDB(dbPath string) (*sql.DB, error) {
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

// migrateFromJSON migrates data from the old JSON/MD files into the new SQLite DB if the DB is empty.
func migrateFromJSON(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath, configPath, scratchPath string) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM config").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // Already migrated or populated
	}

	if err := migrateConfig(ctx, db, fs, configPath); err != nil {
		log.Printf("Failed to migrate config: %v", err)
	}

	if err := migrateTasks(ctx, db, fs, tasksPath); err != nil {
		log.Printf("Failed to migrate tasks: %v", err)
	}

	if err := migrateScratchpad(ctx, db, fs, scratchPath); err != nil {
		log.Printf("Failed to migrate scratchpad: %v", err)
	}

	return nil
}

func migrateConfig(ctx context.Context, db *sql.DB, fs persistence.FileSystem, configPath string) error {
	if stat, _ := fs.Stat(ctx, configPath); stat == nil {
		return nil
	}
	oldConfig := newConfigRepository(fs, configPath)
	configs, err := oldConfig.GetAll(ctx)
	if err != nil || len(configs) == 0 {
		return err
	}
	
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	
	for k, v := range configs {
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)", k, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	
	return tx.Commit()
}

func migrateTasks(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath string) error {
	if stat, _ := fs.Stat(ctx, tasksPath); stat == nil {
		return nil
	}
	oldTasks := newTaskRepository(fs, tasksPath)
	tasks, err := oldTasks.ReadAll(ctx)
	if err != nil || len(tasks) == 0 {
		return err
	}
	
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	
	for _, t := range tasks {
		if _, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
			int64(t.ID), t.Content, t.Status, t.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			return err
		}
	}
	
	return tx.Commit()
}

func migrateScratchpad(ctx context.Context, db *sql.DB, fs persistence.FileSystem, scratchPath string) error {
	if stat, _ := fs.Stat(ctx, scratchPath); stat == nil {
		return nil
	}
	oldScratch := newScratchpadRepository(fs, scratchPath)
	scratch, err := oldScratch.Get(ctx, "content")
	if err != nil || scratch == "" {
		return err
	}
	
	_, err = db.ExecContext(ctx, "INSERT OR REPLACE INTO scratchpad (id, content) VALUES (1, ?)", scratch)
	return err
}
