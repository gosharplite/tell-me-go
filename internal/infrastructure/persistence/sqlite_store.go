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

func (s *sqliteTaskStore) ReadAll(ctx context.Context) ([]ports.Task, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, content, status, created_at FROM tasks ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("querying all tasks: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var res []ports.Task
	for rows.Next() {
		var t ports.Task
		var createdAtStr string
		if err := rows.Scan(&t.ID, &t.Content, &t.Status, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}
		var err error
		t.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at for task %d: %w", int(t.ID), err)
		}
		res = append(res, t)
	}
	if err := rows.Err(); err != nil {
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
