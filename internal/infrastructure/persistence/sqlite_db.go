// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	sqlite "modernc.org/sqlite"
)

// sqlOpenFn is a package-level variable for sql.Open, allowing tests to inject
// failures into the database-open path without modifying the global driver registry.
var sqlOpenFn = sql.Open

// sqliteBusyCode is the SQLITE_BUSY result code (database is locked).
const sqliteBusyCode = 5

// isBusyErr reports whether err is a SQLITE_BUSY error, using typed
// detection (errors.As + result code) rather than string matching.
func isBusyErr(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteBusyCode
}

// createTablesAttempts bounds SQLITE_BUSY retries for schema creation
// (CREATE TABLE IF NOT EXISTS transactions are sub-ms).
const createTablesAttempts = 2

// migrateAttempts bounds SQLITE_BUSY retries for legacy migration; the
// migrate write lock scales with the legacy file (a 100k+ row tasks.json
// holds the lock well over 10 s), so the third attempt converts the
// 10-15 s-hold contention window from failure to success.
const migrateAttempts = 3

// withBusyRetry runs fn, retrying only when fn returns SQLITE_BUSY, with
// exponential backoff (50/100/200 ms base schedule), honoring ctx
// cancellation during backoff. Non-busy errors fail immediately with the
// error chain unchanged. Exhaustion returns the last real (busy) error.
func withBusyRetry(ctx context.Context, fn func() error, attempts int) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := fn(); err != nil {
			if !isBusyErr(err) {
				return err // fail-fast: non-busy, no backoff
			}
			lastErr = err
			if i < attempts-1 {
				delay := time.Duration(50<<i) * time.Millisecond // 50, 100, 200...
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
			continue
		}
		return nil
	}
	return lastErr // exhaustion: last real error
}

// initSQLiteDB opens the SQLite database and runs migrations.
func initSQLiteDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	db, err := sqlOpenFn("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Run schema migrations (single retry layer for SQLITE_BUSY; createTables
	// itself stays pure so attempt counts are unambiguous at this call site).
	if err := withBusyRetry(ctx, func() error { return createTables(ctx, db) }, createTablesAttempts); err != nil {
		_ = db.Close()
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
		return fmt.Errorf("migrating legacy tasks from %s: %w", tasksPath, err)
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
	// NOTE: The deferred Rollback below logs a warning on unexpected rollback
	// failures. This branch is covered by TestMigrateTasks_RollbackWarning
	// using a custom driver.Connector wrapper that injects a Rollback error.
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			logger.Warn("failed to rollback migration transaction", "error", rbErr)
		}
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
			return fmt.Errorf("bulk inserting legacy tasks (batch %d-%d): %w", i, end, err)
		}
	}
	return nil
}
