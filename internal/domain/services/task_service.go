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

// TaskService handles the logic for managing tasks.
type TaskService struct {
	mu     sync.RWMutex
	store  ports.ListStore[ports.Task]
	tasks  map[float64]ports.Task
	nextID float64
}

// NewTaskService creates a new TaskService.
func NewTaskService(store ports.ListStore[ports.Task]) *TaskService {
	return &TaskService{
		store:  store,
		tasks:  make(map[float64]ports.Task),
		nextID: 1,
	}
}

// Initialize loads tasks from the repository.
func (s *TaskService) Initialize(ctx context.Context) error {
	tasks, err := s.store.ReadAll(ctx)
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
func (s *TaskService) AddTask(ctx context.Context, content string) (ports.Task, error) {
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
func (s *TaskService) UpdateTask(ctx context.Context, id float64, content, status string) (ports.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return ports.Task{}, fmt.Errorf("task not found: %.0f", id)
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
func (s *TaskService) DeleteTask(ctx context.Context, id float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("task not found: %.0f", id)
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}

	delete(s.tasks, id)
	return nil
}

// ListTasks returns all tasks, optionally filtered by status.
func (s *TaskService) ListTasks(status string) []ports.Task {
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

	return list
}

// ClearTasks removes all tasks.
func (s *TaskService) ClearTasks(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.DeleteAll(ctx); err != nil {
		return err
	}

	s.tasks = make(map[float64]ports.Task)
	return nil
}
