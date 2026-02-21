// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// TaskRepository manages a list of tasks with persistence.
// It implements services.ListStore[services.Task].
type taskRepository struct {
	mu       sync.RWMutex
	filePath string
	fs       persistence.FileSystem
}

// newTaskRepository creates a new taskRepository.
func newTaskRepository(fs persistence.FileSystem, filePath string) *taskRepository {
	return &taskRepository{
		filePath: filePath,
		fs:       fs,
	}
}

func (r *taskRepository) readAllInternal(ctx context.Context) ([]services.Task, error) {
	if _, err := r.fs.Stat(ctx, r.filePath); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return nil, err
	}

	// Handle empty file
	if len(data) == 0 {
		return nil, nil
	}

	// Try decoding as a JSON array first (backward compatibility/standard JSON)
	var loaded []services.Task
	if data[0] == '[' {
		if err := json.Unmarshal(data, &loaded); err == nil {
			return loaded, nil
		}
	}

	// Fallback to JSONL format
	loaded = nil
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for decoder.More() {
		var t services.Task
		if err := decoder.Decode(&t); err != nil {
			return nil, err
		}
		loaded = append(loaded, t)
	}

	return loaded, nil
}

// ReadAll loads tasks from disk.
func (r *taskRepository) ReadAll(ctx context.Context) ([]services.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readAllInternal(ctx)
}

func (r *taskRepository) writeAllInternal(ctx context.Context, tasks []services.Task) error {
	// Sort by ID stable
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	var data []byte
	for _, t := range tasks {
		line, err := json.Marshal(t)
		if err != nil {
			return err
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	return r.fs.WriteFile(ctx, r.filePath, data, 0644)
}

// Update modifies an existing task on disk.
func (r *taskRepository) Update(ctx context.Context, id float64, item services.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.readAllInternal(ctx)
	if err != nil {
		return err
	}

	for i, t := range tasks {
		if t.ID == id {
			tasks[i] = item
			return r.writeAllInternal(ctx, tasks)
		}
	}
	return nil
}

// Delete removes a task from disk.
func (r *taskRepository) Delete(ctx context.Context, id float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.readAllInternal(ctx)
	if err != nil {
		return err
	}

	var next []services.Task
	for _, t := range tasks {
		if t.ID != id {
			next = append(next, t)
		}
	}
	return r.writeAllInternal(ctx, next)
}

// DeleteAll removes all tasks from disk.
func (r *taskRepository) DeleteAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeAllInternal(ctx, nil)
}

// Append appends a single task to disk.
func (r *taskRepository) Append(ctx context.Context, task services.Task) (err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	f, oerr := r.fs.OpenFile(ctx, r.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oerr != nil {
		return oerr
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	line, merr := json.Marshal(task)
	if merr != nil {
		return merr
	}
	line = append(line, '\n')

	_, err = f.Write(line)
	return err
}
