// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// promptEntry represents a single entry in the global prompt log.
type promptEntry struct {
	Timestamp string `json:"timestamp"`
	Prompt    string `json:"prompt"`
}

var _ ports.PromptTracker = (*globalPromptTracker)(nil)

// globalPromptTracker handles atomic, lock-free recording of user prompts.
type globalPromptTracker struct {
	filepath string
}

// NewGlobalPromptTracker creates a new tracker pointing to the specified home directory.
func NewGlobalPromptTracker(homeDir string) ports.PromptTracker {
	return &globalPromptTracker{
		filepath: filepath.Join(homeDir, "global_prompts.jsonl"),
	}
}

// Append records a new prompt to the global log file.
// Uses os.O_APPEND for atomic writes on POSIX and Windows (up to OS-specific limits).
func (t *globalPromptTracker) Append(prompt string) error {
	if prompt == "" {
		return nil
	}

	entry := promptEntry{
		Timestamp: time.Now().Format(time.RFC3339),
		Prompt:    prompt,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal prompt entry: %w", err)
	}

	f, err := os.OpenFile(t.filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open global prompts file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to append prompt: %w", err)
	}

	return nil
}

// LoadTopN loads the last unique prompts up to the limit using reverse reading.
// Time complexity: O(limit * avgLineLen), Memory complexity: O(limit * avgLineLen).
func (t *globalPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	f, err := os.Open(t.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open global prompts file: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat global prompts file: %w", err)
	}

	pos := info.Size()
	if pos == 0 {
		return nil, nil
	}

	const chunkSize = 4096
	seen := make(map[string]bool)
	result := make([]string, 0, limit)
	var leftover []byte

	for pos > 0 && len(result) < limit {
		// Periodically check for context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		readSize := chunkSize
		if pos < int64(readSize) {
			readSize = int(pos)
		}
		pos -= int64(readSize)

		chunk := make([]byte, readSize)
		if _, err := f.ReadAt(chunk, pos); err != nil {
			return nil, fmt.Errorf("failed to read global prompts at %d: %w", pos, err)
		}

		// Combine with leftover from previous chunk
		data := append(chunk, leftover...)
		lines := bytes.Split(data, []byte{'\n'})

		if pos > 0 {
			leftover = lines[0]
			lines = lines[1:]
		} else {
			leftover = nil
		}

		// Process lines in reverse order (most recent first)
		for i := len(lines) - 1; i >= 0; i-- {
			if len(lines[i]) == 0 {
				continue
			}

			var entry promptEntry
			if err := json.Unmarshal(lines[i], &entry); err == nil {
				p := entry.Prompt
				if p != "" && !seen[p] {
					seen[p] = true
					result = append(result, p)
					if len(result) >= limit {
						break
					}
				}
			}
		}
	}

	return result, nil
}
