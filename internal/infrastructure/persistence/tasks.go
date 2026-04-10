// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bufio"
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
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		return nil, nil
	}

	// Try decoding as a JSON array first (backward compatibility/standard JSON)
	if trimmed[0] == '[' {
		var loaded []ports.Task
		if err := json.Unmarshal([]byte(trimmed), &loaded); err == nil {
			return loaded, nil
		}
	}

	// Fallback to JSONL format
	var loaded []ports.Task
	scanner := bufio.NewScanner(strings.NewReader(trimmed))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var t ports.Task
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			// Skip corrupted lines in legacy tasks to ensure boot continues.
			// This handles cases where log lines or other non-JSON data may have leaked into the file.
			// [DEBUG] Log corrupted lines to help identify the source of leakage on Windows.
			if strings.Contains(os.Getenv("TELL_ME_DEBUG"), "migration") {
				fmt.Printf("DEBUG: corrupted task line in %s: %q\n", r.filePath, line)
			}
			continue
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

func (r *taskRepository) writeAllInternal(ctx context.Context, tasks []ports.Task) error {
	// Write as JSONL
	var sb strings.Builder
	for _, t := range tasks {
		data, err := json.Marshal(t)
		if err != nil {
			return err
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	return r.fs.AtomicWrite(ctx, r.filePath, []byte(sb.String()), 0644)
}

// Append adds a new task.
func (r *taskRepository) Append(ctx context.Context, item ports.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.readAllInternal(ctx)
	if err != nil {
		return err
	}

	tasks = append(tasks, item)
	return r.writeAllInternal(ctx, tasks)
}

// Update modifies an existing task.
func (r *taskRepository) Update(ctx context.Context, id float64, item ports.Task) error {
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
	return ports.ErrTaskNotFound
}

// Delete removes a task.
func (r *taskRepository) Delete(ctx context.Context, id float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tasks, err := r.readAllInternal(ctx)
	if err != nil {
		return err
	}

	for i, t := range tasks {
		if t.ID == id {
			tasks = append(tasks[:i], tasks[i+1:]...)
			return r.writeAllInternal(ctx, tasks)
		}
	}
	return ports.ErrTaskNotFound
}

// DeleteAll clears all tasks.
func (r *taskRepository) DeleteAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fs.AtomicWrite(ctx, r.filePath, []byte(""), 0644)
}
