// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"database/sql"
	"fmt"
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

func (s *sqliteTaskStore) ReadAll(ctx context.Context) (res []ports.Task, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, content, status, created_at FROM tasks ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("querying all tasks: %w", err)
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
		if scanErr := rows.Scan(&t.ID, &t.Content, &t.Status, &createdAtStr); scanErr != nil {
			return nil, fmt.Errorf("scanning task row: %w", scanErr)
		}
		var parseErr error
		t.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAtStr)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse created_at for task %d: %w", int(t.ID), parseErr)
		}
		res = append(res, t)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("tasks rows iteration error: %w", err)
	}
	return res, nil
}

func (s *sqliteTaskStore) Update(ctx context.Context, id float64, item ports.Task) error {
	_, err := s.db.ExecContext(ctx, "UPDATE tasks SET content = ?, status = ? WHERE id = ?",
		item.Content, item.Status, int64(id))
	if err != nil {
		return fmt.Errorf("updating task %d: %w", int(id), err)
	}
	return nil
}

func (s *sqliteTaskStore) Delete(ctx context.Context, id float64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", int64(id))
	if err != nil {
		return fmt.Errorf("deleting task %d: %w", int(id), err)
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
		int64(item.ID), item.Content, item.Status, item.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("appending task %d: %w", int(item.ID), err)
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
