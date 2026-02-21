// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"os"
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

func (r *taskRepository) ReadPage(ctx context.Context, limit, offset int) ([]services.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all, err := r.readAllInternal(ctx)
	if err != nil {
		return nil, err
	}
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	res := make([]services.Task, end-offset)
	copy(res, all[offset:end])
	return res, nil
}
