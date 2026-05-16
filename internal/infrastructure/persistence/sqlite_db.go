// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	_ "modernc.org/sqlite"
)

// sqlOpenFn is a package-level variable for sql.Open, allowing tests to inject
// failures into the database-open path without modifying the global driver registry.
var sqlOpenFn = sql.Open

// initSQLiteDB opens the SQLite database and runs migrations.
func initSQLiteDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	db, err := sqlOpenFn("sqlite", dbPath)
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
func migrateFromJSON(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath string, logger *slog.Logger) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
		return fmt.Errorf("checking tasks table: %w", err)
	}
	if count > 0 {
		return nil // Already migrated or populated
	}

	if err := migrateTasks(ctx, db, fs, tasksPath, logger); err != nil {
		log.Printf("Failed to migrate tasks: %v", err)
	}

	return nil
}

func migrateTasks(ctx context.Context, db *sql.DB, fs persistence.FileSystem, tasksPath string, logger *slog.Logger) error {
	stat, err := fs.Stat(ctx, tasksPath)
	if err != nil || stat.IsDir() {
		return nil
	}

	oldTasks := newTaskRepository(fs, tasksPath, logger)
	tasks, err := oldTasks.ReadAll(ctx)
	if err != nil || len(tasks) == 0 {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	rows := mapTasksToRows(tasks)
	if err := executeBatchInsert(ctx, tx, rows); err != nil {
		return err
	}

	return tx.Commit()
}

type taskRow struct {
	ID        int64
	Content   string
	Status    string
	CreatedAt string
}

func mapTasksToRows(tasks []ports.Task) []taskRow {
	rows := make([]taskRow, len(tasks))
	for i, t := range tasks {
		rows[i] = taskRow{
			ID:        int64(t.ID),
			Content:   t.Content,
			Status:    t.Status,
			CreatedAt: t.CreatedAt.Format(time.RFC3339Nano),
		}
	}
	return rows
}

func executeBatchInsert(ctx context.Context, tx *sql.Tx, rows []taskRow) error {
	batchSize := 200
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		var queryBuilder strings.Builder
		queryBuilder.WriteString("INSERT OR REPLACE INTO tasks (id, content, status, created_at) VALUES ")
		var args []interface{}

		for j, r := range batch {
			if j > 0 {
				queryBuilder.WriteString(", ")
			}
			queryBuilder.WriteString("(?, ?, ?, ?)")
			args = append(args, r.ID, r.Content, r.Status, r.CreatedAt)
		}

		if _, err := tx.ExecContext(ctx, queryBuilder.String(), args...); err != nil {
			return fmt.Errorf("bulk inserting legacy tasks: %w", err)
		}
	}
	return nil
}
