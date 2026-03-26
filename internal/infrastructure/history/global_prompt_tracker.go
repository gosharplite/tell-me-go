// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to append prompt: %w", err)
	}

	return nil
}

// LoadTopN loads the last unique prompts up to the limit.
func (t *GlobalPromptTracker) LoadTopN(limit int) ([]string, error) {
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
	defer f.Close()

	// Read all prompts into a list (this is a simple implementation)
	// For very large files, this would need optimization.
	var prompts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry PromptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Skip malformed lines
		}
		prompts = append(prompts, entry.Prompt)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read global prompts: %w", err)
	}

	// Reverse and filter duplicates
	seen := make(map[string]bool)
	var result []string
	for i := len(prompts) - 1; i >= 0; i-- {
		p := prompts[i]
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
			if len(result) >= limit {
				break
			}
		}
	}

	return result, nil
}
