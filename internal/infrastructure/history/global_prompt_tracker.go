// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// PromptEntry represents a single entry in the global prompt log.
type PromptEntry struct {
	Timestamp string `json:"timestamp"`
	Prompt    string `json:"prompt"`
}

// GlobalPromptTracker handles atomic, lock-free recording of user prompts.
type GlobalPromptTracker struct {
	filepath string
}

// NewGlobalPromptTracker creates a new tracker pointing to the specified home directory.
func NewGlobalPromptTracker(homeDir string) *GlobalPromptTracker {
	return &GlobalPromptTracker{
		filepath: filepath.Join(homeDir, "global_prompts.jsonl"),
	}
}

// Append records a new prompt to the global log file.
// Uses os.O_APPEND for atomic writes on POSIX and Windows (up to OS-specific limits).
func (t *GlobalPromptTracker) Append(prompt string) error {
	if prompt == "" {
		return nil
	}

	entry := PromptEntry{
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

// LoadTopN loads the last unique prompts up to the limit using a bounded circular buffer.
// Time complexity: O(FileLines), Memory complexity: O(limit).
func (t *GlobalPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
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

	// Circular buffer to store the last 'limit' lines.
	buffer := make([]string, limit)
	count := 0
	reader := bufio.NewReader(f)

	for {
		// Periodically check for context cancellation
		if count%100 == 0 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				if len(line) > 0 {
					buffer[count%limit] = string(line)
					count++
				}
				break
			}
			return nil, fmt.Errorf("failed to read global prompts: %w", err)
		}
		buffer[count%limit] = string(line)
		count++
	}

	// Extract unique items from the circular buffer (newest first).
	size := limit
	if count < limit {
		size = count
	}

	seen := make(map[string]bool)
	var result []string

	for i := 0; i < size; i++ {
		// Calculate the index of the (count - 1 - i)-th element
		idx := (count - 1 - i) % limit
		if idx < 0 {
			idx += limit
		}

		var entry PromptEntry
		if err := json.Unmarshal([]byte(buffer[idx]), &entry); err == nil {
			p := entry.Prompt
			if p != "" && !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}

	return result, nil
}
