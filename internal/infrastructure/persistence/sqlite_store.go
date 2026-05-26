// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	_ "modernc.org/sqlite"
)

// sqliteTaskStore implements ListStore[Task] using SQLite.
type sqliteTaskStore struct {
	db *sql.DB
}

func newSQLiteTaskStore(db *sql.DB) *sqliteTaskStore {
	return &sqliteTaskStore{db: db}
}

func (s *sqliteTaskStore) ReadAll(ctx context.Context) ([]ports.Task, error) {
	// Load all active tasks (not completed) — unbounded count is acceptable
	// because active tasks are bounded by human workflow.
	active, err := s.Query(ctx, ports.ListFilter{NotStatus: "completed"}, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("querying active tasks: %w", err)
	}

	// Load most recent 500 completed tasks (ORDER BY id DESC, LIMIT 500).
	completed, err := s.queryOrdered(ctx, ports.ListFilter{Status: "completed"}, 500, 0, "DESC")
	if err != nil {
		return nil, fmt.Errorf("querying completed tasks: %w", err)
	}

	// Merge: active first (already sorted ASC by id), then completed reversed to ASC.
	all := make([]ports.Task, 0, len(active)+len(completed))
	all = append(all, active...)

	// completed comes back DESC (most recent first). Reverse to ASC before appending.
	for i := len(completed) - 1; i >= 0; i-- {
		all = append(all, completed[i])
	}

	return all, nil
}

// Query returns tasks matching the filter. Results are ordered by id ASC.
// Use queryOrdered for DESC ordering.
func (s *sqliteTaskStore) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]ports.Task, error) {
	return s.queryOrdered(ctx, filter, limit, offset, "ASC")
}

// whereClause holds a parameterized SQL WHERE fragment and its arguments.
type whereClause struct {
	sql  string
	args []any
}

// buildWhereClause constructs a WHERE fragment from the given ListFilter.
// Returns an empty whereClause if no filter conditions are set.
func buildWhereClause(filter ports.ListFilter) whereClause {
	var conditions []string
	var args []any

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.NotStatus != "" {
		conditions = append(conditions, "status != ?")
		args = append(args, filter.NotStatus)
	}
	if !filter.Since.IsZero() {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.Since.Format(time.RFC3339Nano))
	}
	if !filter.Before.IsZero() {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.Before.Format(time.RFC3339Nano))
	}

	if len(conditions) == 0 {
		return whereClause{}
	}

	return whereClause{
		sql:  " WHERE " + strings.Join(conditions, " AND "),
		args: args,
	}
}

// orderClause holds a parameterized SQL ORDER BY / LIMIT / OFFSET fragment.
type orderClause struct {
	sql  string
	args []any
}

// buildOrderClause constructs an ORDER BY fragment with optional LIMIT and OFFSET.
// Defaults to "ORDER BY id ASC" when order is not "DESC".
func buildOrderClause(order string, limit, offset int) orderClause {
	var sql string
	var args []any

	if order == "DESC" {
		sql = " ORDER BY id DESC"
	} else {
		sql = " ORDER BY id ASC"
	}

	if limit > 0 {
		sql += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		sql += " OFFSET ?"
		args = append(args, offset)
	}

	return orderClause{sql: sql, args: args}
}

// queryOrdered is like Query but accepts an explicit ORDER direction ("ASC" or "DESC").
func (s *sqliteTaskStore) queryOrdered(ctx context.Context, filter ports.ListFilter, limit, offset int, order string) (result []ports.Task, err error) {
	wc := buildWhereClause(filter)
	oc := buildOrderClause(order, limit, offset)

	query := "SELECT id, content, status, created_at FROM tasks" + wc.sql + oc.sql
	args := append(wc.args, oc.args...)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks ordered %s: %w", order, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
	}()
	for rows.Next() {
		var t ports.Task
		var createdAtStr string
		if err := rows.Scan(&t.ID, &t.Content, &t.Status, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}
		t.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at for task %d: %w", int(t.ID), err)
		}
		result = append(result, t)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("tasks rows iteration error: %w", err)
	}
	return result, nil
}

func (s *sqliteTaskStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting tasks: %w", err)
	}
	return count, nil
}

func (s *sqliteTaskStore) Update(ctx context.Context, id int64, item ports.Task) error {
	_, err := s.db.ExecContext(ctx, "UPDATE tasks SET content = ?, status = ? WHERE id = ?",
		item.Content, item.Status, id)
	if err != nil {
		return fmt.Errorf("updating task %d: %w", id, err)
	}
	return nil
}

func (s *sqliteTaskStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting task %d: %w", id, err)
	}
	return nil
}

func (s *sqliteTaskStore) DeleteAll(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tasks")
	if err != nil {
		return fmt.Errorf("deleting all tasks: %w", err)
	}
	return nil
}

func (s *sqliteTaskStore) Append(ctx context.Context, item ports.Task) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO tasks (id, content, status, created_at) VALUES (?, ?, ?, ?)",
		item.ID, item.Content, item.Status, item.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("appending task %d: %w", item.ID, err)
	}
	return nil
}

type sqliteKVStore struct {
	db *sql.DB
}

func newSQLiteKVStore(db *sql.DB) *sqliteKVStore {
	return &sqliteKVStore{db: db}
}

func (s *sqliteKVStore) Get(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("getting setting %s: %w", key, err)
	}
	return val, nil
}

func (s *sqliteKVStore) Set(ctx context.Context, key string, val string) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, val)
	if err != nil {
		return fmt.Errorf("setting %s: %w", key, err)
	}
	return nil
}

func (s *sqliteKVStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", key)
	if err != nil {
		return fmt.Errorf("deleting setting %s: %w", key, err)
	}
	return nil
}

func (s *sqliteKVStore) GetAll(ctx context.Context) (res map[string]string, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("querying all settings: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
		}
	}()

	res = make(map[string]string)
	for rows.Next() {
		var k, v string
		if scanErr := rows.Scan(&k, &v); scanErr != nil {
			return nil, fmt.Errorf("scanning setting row: %w", scanErr)
		}
		res[k] = v
	}

	// Check for errors that occurred during iteration
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating settings rows: %w", err)
	}

	return res, nil
}
