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
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// promptEntry represents a single entry in the global prompt log.
type promptEntry struct {
	Timestamp string `json:"timestamp"`
	Prompt    string `json:"prompt"`
}

var _ ports.PromptTracker = (*globalPromptTracker)(nil)

const (
	maxGlobalPrompts         = 1200
	compactionThresholdBytes = 150 * 1024
)

// globalPromptTracker handles atomic recording of user prompts.
type globalPromptTracker struct {
	filepath   string
	compacting atomic.Bool
	mu         sync.RWMutex
}

// NewGlobalPromptTracker creates a new tracker pointing to the specified home directory.
func NewGlobalPromptTracker(homeDir string) ports.PromptTracker {
	// Ensure the directory exists
	dir := filepath.Join(homeDir, ".tellmego")
	_ = os.MkdirAll(dir, 0755)

	trackerPath := filepath.Join(dir, "prompts.jsonl")

	// --- NEW CODE: Cleanup orphaned temp files from previous hard crashes ---
	pattern := filepath.Join(dir, filepath.Base(trackerPath)+".tmp-*")
	if matches, err := filepath.Glob(pattern); err == nil {
		for _, match := range matches {
			_ = os.Remove(match) // Best-effort cleanup
		}
	}
	// --- END NEW CODE ---

	return &globalPromptTracker{
		filepath: trackerPath,
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

	t.mu.RLock()
	defer t.mu.RUnlock()

	f, err := os.OpenFile(t.filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open global prompts file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to append prompt: %w", err)
	}

	var size int64
	if info, err := f.Stat(); err == nil {
		size = info.Size()
	}

	// Trigger async compaction if file size exceeds threshold and no compaction is already in progress
	if size > compactionThresholdBytes {
		if t.compacting.CompareAndSwap(false, true) {
			go t.compactLog()
		}
	}

	return nil
}

// LoadTopN loads the last unique prompts up to the limit using reverse reading.
// Time complexity: O(limit * avgLineLen), Memory complexity: O(limit * avgLineLen).
func (t *globalPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}

	entries, err := t.loadTopUniqueEntries(ctx, limit)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(entries))
	for _, e := range entries {
		result = append(result, e.Prompt)
	}
	return result, nil
}

func (t *globalPromptTracker) loadTopUniqueEntries(ctx context.Context, limit int) ([]promptEntry, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.doLoadTopUniqueEntries(ctx, limit)
}

func (t *globalPromptTracker) doLoadTopUniqueEntries(ctx context.Context, limit int) ([]promptEntry, error) {
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
	result := make([]promptEntry, 0, limit)
	var leftover []byte

	for pos > 0 && len(result) < limit {
		// Periodically check for context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		chunk, err := t.readPreviousChunk(f, &pos, chunkSize)
		if err != nil {
			return nil, err
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
					result = append(result, entry)
					if len(result) >= limit {
						break
					}
				}
			}
		}
	}

	return result, nil
}

func (t *globalPromptTracker) compactLog() {
	defer t.compacting.Store(false)

	t.mu.Lock()
	defer t.mu.Unlock()

	entries, err := t.doLoadTopUniqueEntries(context.Background(), maxGlobalPrompts)
	if err != nil || len(entries) == 0 {
		return
	}

	// Reverse entries to chronological order (oldest first)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// Use a unique temporary file to avoid races between multiple compaction attempts
	tmpFile, err := os.CreateTemp(filepath.Dir(t.filepath), filepath.Base(t.filepath)+".tmp-*")
	if err != nil {
		return
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := tmpFile.Write(append(data, '\n')); err != nil {
			return
		}
	}

	if err := tmpFile.Close(); err != nil {
		return
	}

	_ = os.Rename(tmpPath, t.filepath)
}

func (t *globalPromptTracker) readPreviousChunk(f *os.File, pos *int64, chunkSize int) ([]byte, error) {
	readSize := chunkSize
	if *pos < int64(readSize) {
		readSize = int(*pos)
	}
	*pos -= int64(readSize)

	chunk := make([]byte, readSize)
	if _, err := f.ReadAt(chunk, *pos); err != nil {
		return nil, fmt.Errorf("failed to read global prompts at %d: %w", *pos, err)
	}
	return chunk, nil
}
