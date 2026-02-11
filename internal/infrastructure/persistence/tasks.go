// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// TaskRepository manages a list of tasks with persistence.
// It implements services.ListStore[services.Task].
type TaskRepository struct {
	mu       sync.RWMutex
	filePath string
	fs       storage.FileSystem
}

// NewTaskRepository creates a new TaskRepository.
func NewTaskRepository(fs storage.FileSystem, filePath string) *TaskRepository {
	return &TaskRepository{
		filePath: filePath,
		fs:       fs,
	}
}

// ReadAll loads tasks from disk.
func (r *TaskRepository) ReadAll(ctx context.Context) ([]services.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.fs.Stat(ctx, r.filePath); err != nil {
		return nil, nil // File doesn't exist yet, which is fine
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return nil, err
	}

	var loaded []services.Task
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, err
	}

	return loaded, nil
}

// WriteAll saves tasks to disk.
func (r *TaskRepository) WriteAll(ctx context.Context, tasks []services.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Sort by ID stable
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}
