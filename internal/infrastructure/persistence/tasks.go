// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// TaskRepository manages a list of tasks with persistence.
// It implements ports.ListStore[ports.Task].
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

func (r *taskRepository) readAllInternal(ctx context.Context) ([]ports.Task, error) {
	if _, err := r.fs.Stat(ctx, r.filePath); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := r.fs.ReadFile(ctx, r.filePath)
	if err != nil {
		return nil, fmt.Errorf("reading tasks file %s: %w", r.filePath, err)
	}

	// Handle empty file
	if len(data) == 0 {
		return nil, nil
	}

	// Try decoding as a JSON array first (backward compatibility/standard JSON)
	var loaded []ports.Task
	if data[0] == '[' {
		if err := json.Unmarshal(data, &loaded); err == nil {
			return loaded, nil
		}
	}

	// Fallback to JSONL format
	loaded = nil
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for decoder.More() {
		var t ports.Task
		if err := decoder.Decode(&t); err != nil {
			return nil, fmt.Errorf("decoding task: %w", err)
		}
		loaded = append(loaded, t)
	}

	return loaded, nil
}

// ReadAll loads tasks from disk.
func (r *taskRepository) ReadAll(ctx context.Context) ([]ports.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readAllInternal(ctx)
}
