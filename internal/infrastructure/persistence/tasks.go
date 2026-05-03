// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// taskRepository manages a list of tasks for migration from legacy storage.
type taskRepository struct {
	mu       sync.RWMutex
	filePath string
	fs       persistence.FileSystem
	logger   *slog.Logger
}

// newTaskRepository creates a new taskRepository.
func newTaskRepository(fs persistence.FileSystem, filePath string, logger *slog.Logger) *taskRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &taskRepository{
		filePath: filePath,
		fs:       fs,
		logger:   logger,
	}
}

// parseJSONLTasks parses tasks from JSONL format (one JSON object per line).
// Corrupted lines are skipped; debug logging is emitted when TELL_ME_DEBUG=migration.
func parseJSONLTasks(trimmed string, filePath string, logger *slog.Logger) []ports.Task {
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
				logger.Debug("corrupted task line during migration",
					slog.String("path", filePath),
					slog.String("line", line),
					slog.Any("error", err))
			}
			continue
		}
		loaded = append(loaded, t)
	}
	return loaded
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
	loaded := parseJSONLTasks(trimmed, r.filePath, r.logger)
	return loaded, nil
}

// ReadAll loads tasks from disk.
func (r *taskRepository) ReadAll(ctx context.Context) ([]ports.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readAllInternal(ctx)
}
