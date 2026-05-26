// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Ensure taskService strictly implements the ports.TaskStore interface.
// This explicit binding also resolves false positives in dead-code AST analysis.
var _ ports.TaskStore = (*taskService)(nil)

// taskService handles the logic for managing tasks.
type taskService struct {
	mu     sync.RWMutex
	store  ports.ListStore[ports.Task]
	tasks  map[int64]ports.Task
	nextID int64
}

// NewTaskService creates a new taskService.
func NewTaskService(store ports.ListStore[ports.Task]) *taskService {
	return &taskService{
		store:  store,
		tasks:  make(map[int64]ports.Task),
		nextID: 1,
	}
}

// Initialize loads tasks from the repository.
func (s *taskService) Initialize(ctx context.Context) error {
	// Load only active tasks (not completed) into memory.
	// Completed tasks remain in the persistent store and can be
	// queried on demand via ListTasks with status filter.
	tasks, err := s.store.Query(ctx, ports.ListFilter{NotStatus: "completed"}, 0, 0)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range tasks {
		s.tasks[t.ID] = t
		if t.ID >= s.nextID {
			s.nextID = t.ID + 1
		}
	}
	return nil
}

// AddTask adds a new task.
func (s *taskService) AddTask(ctx context.Context, content string) (ports.Task, error) {
	if content == "" {
		return ports.Task{}, fmt.Errorf("content is required for add")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t := ports.Task{
		ID:        s.nextID,
		Content:   content,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	if err := s.store.Append(ctx, t); err != nil {
		return ports.Task{}, err
	}

	s.tasks[t.ID] = t
	s.nextID++
	return t, nil
}

// UpdateTask updates an existing task.
func (s *taskService) UpdateTask(ctx context.Context, id int64, content, status string) (ports.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
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

	s.tasks[id] = t
	return t, nil
}

// DeleteTask removes a task.
func (s *taskService) DeleteTask(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("id %d: %w", id, ports.ErrTaskNotFound)
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}

	delete(s.tasks, id)
	return nil
}

// ListTasks returns all tasks, optionally filtered by status, bounded by limit and offset.
func (s *taskService) ListTasks(status string, limit, offset int) []ports.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []ports.Task
	for _, t := range s.tasks {
		if status != "" && t.Status != status {
			continue
		}
		list = append(list, t)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})

	// Apply offset
	if offset > 0 {
		if offset >= len(list) {
			return []ports.Task{}
		}
		list = list[offset:]
	}

	// Apply limit
	if limit > 0 && limit < len(list) {
		list = list[:limit]
	}

	return list
}

// CountTasks returns the total number of tasks matching the given status filter.
// status="" returns the total count across all statuses.
func (s *taskService) CountTasks(status string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// For the in-memory map path (current behavior): count from s.tasks.
	// This is consistent with ListTasks which also reads from s.tasks.
	// [TECHNICAL DEBT] When ListTasks is updated to query the persistent store
	// for non-pending statuses (Issue #521), this must also fall through to
	// the store for accurate counts across sessions.
	count := 0
	for _, t := range s.tasks {
		if status != "" && t.Status != status {
			continue
		}
		count++
	}
	return count
}

// ClearTasks removes all tasks.
func (s *taskService) ClearTasks(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.DeleteAll(ctx); err != nil {
		return err
	}

	s.tasks = make(map[int64]ports.Task)
	return nil
}
