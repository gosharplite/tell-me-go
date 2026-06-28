// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Ensure taskService strictly implements the ports.TaskStore interface.
// This explicit binding also resolves false positives in dead-code AST analysis.
var _ ports.TaskStore = (*taskService)(nil)

// taskIDCounter provides monotonically increasing task identifiers.
// An atomic counter replaces time.Now().UnixNano() which can return
// identical values on systems where wall-clock resolution is coarser
// than a nanosecond (e.g., macOS with ~1µs resolution).
var taskIDCounter int64

func nextTaskID() int64 {
	return atomic.AddInt64(&taskIDCounter, 1)
}

// taskService handles the logic for managing tasks.
// It is a stateless pass-through that delegates all operations to the store.
type taskService struct {
	store ports.ListStore[ports.Task]
}

// NewTaskService creates a new taskService.
func NewTaskService(store ports.ListStore[ports.Task]) *taskService {
	return &taskService{store: store}
}

// Initialize is a no-op required for ports.Initializer compatibility.
// All state is managed by the backing store.
func (s *taskService) Initialize(ctx context.Context) error { return nil }

// AddTask adds a new task.
func (s *taskService) AddTask(ctx context.Context, content string) (ports.Task, error) {
	if content == "" {
		return ports.Task{}, fmt.Errorf("content is required for add")
	}

	t := ports.Task{
		ID:        nextTaskID(),
		Content:   content,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	if err := s.store.Append(ctx, t); err != nil {
		return ports.Task{}, err
	}

	return t, nil
}

// UpdateTask updates an existing task.
func (s *taskService) UpdateTask(ctx context.Context, id int64, content, status string) (ports.Task, error) {
	// Fetch existing tasks from store to validate existence
	tasks, err := s.store.Query(ctx, ports.ListFilter{}, 0, 0)
	if err != nil {
		return ports.Task{}, err
	}

	var t ports.Task
	found := false
	for _, existing := range tasks {
		if existing.ID == id {
			t = existing
			found = true
			break
		}
	}
	if !found {
		return ports.Task{}, fmt.Errorf("id %d: %w", id, ports.ErrTaskNotFound)
	}

	if content != "" {
		t.Content = content
	}
	if status != "" {
		t.Status = status
	}

	if err := s.store.Update(ctx, id, t); err != nil {
		return ports.Task{}, err
	}

	return t, nil
}

// DeleteTask removes a task.
func (s *taskService) DeleteTask(ctx context.Context, id int64) error {
	// Pre-check existence
	tasks, err := s.store.Query(ctx, ports.ListFilter{}, 0, 0)
	if err != nil {
		return err
	}
	found := false
	for _, t := range tasks {
		if t.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("id %d: %w", id, ports.ErrTaskNotFound)
	}
	return s.store.Delete(ctx, id)
}

// ListTasks returns all tasks, optionally filtered by status, bounded by limit and offset.
func (s *taskService) ListTasks(ctx context.Context, status string, limit, offset int) ([]ports.Task, error) {
	filter := ports.ListFilter{Status: status}
	return s.store.Query(ctx, filter, limit, offset)
}

// CountTasks returns the total number of tasks matching the given status filter.
// status="" returns the total count across all statuses.
func (s *taskService) CountTasks(ctx context.Context, status string) (int, error) {
	filter := ports.ListFilter{Status: status}
	tasks, err := s.store.Query(ctx, filter, 0, 0)
	if err != nil {
		return 0, err
	}
	return len(tasks), nil
}

// ClearTasks removes all tasks.
func (s *taskService) ClearTasks(ctx context.Context) error {
	return s.store.DeleteAll(ctx)
}
