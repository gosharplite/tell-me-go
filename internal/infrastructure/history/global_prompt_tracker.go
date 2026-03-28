// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	filepath           string
	compacting         atomic.Bool
	mu                 sync.RWMutex
	testCompactionHook func()
}

// NewGlobalPromptTracker creates a new tracker pointing to the specified home directory.
func NewGlobalPromptTracker(homeDir string) (ports.PromptTracker, error) {
	// Ensure the directory exists
	dir := filepath.Join(homeDir, ".tellmego")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for prompt tracker: %w", err)
	}

	trackerPath := filepath.Join(dir, "prompts.jsonl")
	oldTrackerPath := filepath.Join(homeDir, "global_prompts.jsonl")

	tracker := &globalPromptTracker{
		filepath: trackerPath,
	}

	// Migrate old prompts file to the new location to prevent data loss
	if _, err := os.Stat(trackerPath); os.IsNotExist(err) {
		if _, err := os.Stat(oldTrackerPath); err == nil {
			// Robust: Attempt rename, fallback to copy+delete
			err := os.Rename(oldTrackerPath, trackerPath)
			if err != nil {
				// Fallback for EXDEV (cross-device link) or other rename failures
				if copyErr := copyFile(oldTrackerPath, trackerPath); copyErr != nil {
					return tracker, fmt.Errorf("failed to migrate history file: %w", copyErr)
				}
				// Only remove the old file if the copy was successful
				_ = os.Remove(oldTrackerPath)
			}
		}
	}

	return tracker, nil
}

// Append records a new prompt to the global log file.
// Uses os.O_APPEND for atomic writes on POSIX and Windows (up to OS-specific limits).
func (t *globalPromptTracker) Append(ctx context.Context, prompt string) error {
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

	t.mu.Lock()
	defer t.mu.Unlock()

	f, err := os.OpenFile(t.filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open global prompts file: %w", err)
	}
	defer func() { _ = f.Close() }()

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
			go t.compactLog(context.WithoutCancel(ctx))
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
		result = t.processReversedLines(lines, seen, result, limit)
	}

	return result, nil
}

// processReversedLines iterates through the lines backwards, unmarshals them, deduplicates, and appends to results.
func (t *globalPromptTracker) processReversedLines(lines [][]byte, seen map[string]bool, result []promptEntry, limit int) []promptEntry {
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
	return result
}

func (t *globalPromptTracker) compactLog(ctx context.Context) {
	defer t.compacting.Store(false)
	if t.testCompactionHook != nil {
		defer t.testCompactionHook()
	}

	const maxRetries = 3
	backoff := 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if t.performCompactionPass(ctx) {
			return
		}

		// If we aborted because of concurrent writes, check if we still need compaction.
		// If so, loop and try again. This ensures that a burst of appends doesn't
		// leave the file uncompacted just because the last append arrived while
		// a previous compaction attempt was finishing.
		newInfo, err := os.Stat(t.filepath)
		if err != nil || newInfo.Size() <= compactionThresholdBytes {
			return
		}

		// Scalable: Allows immediate exit if the application is shutting down
		select {
		case <-ctx.Done():
			return // Graceful shutdown
		case <-time.After(backoff):
			backoff *= 2 // Exponential backoff is safer for I/O locks
		}
	}
}

// performCompactionPass attempts a single optimistic compaction pass.
// It returns true if successful, false if aborted due to concurrent writes or errors.
func (t *globalPromptTracker) performCompactionPass(ctx context.Context) bool {
	// Capture initial size for optimistic concurrency check
	info, err := os.Stat(t.filepath)
	if err != nil {
		return false
	}
	initialSize := info.Size()

	// 1. Read entries without holding the lock to allow concurrent appends
	entries, err := t.doLoadTopUniqueEntries(ctx, maxGlobalPrompts)
	if err != nil || len(entries) == 0 {
		return false
	}

	// Reverse entries to chronological order (oldest first)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// 2. Write to temp file
	tmpFile, err := os.CreateTemp(filepath.Dir(t.filepath), filepath.Base(t.filepath)+".tmp-*")
	if err != nil {
		return false
	}
	tmpPath := tmpFile.Name()

	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if !t.writeCompactedTempFile(tmpFile, entries) {
		return false
	}

	if err := tmpFile.Close(); err != nil {
		return false
	}

	// 3. Final check and swap under exclusive lock
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if file size has changed (meaning Append was called in the background)
	newInfo, err := os.Stat(t.filepath)
	if err != nil || newInfo.Size() != initialSize {
		return false // Abort this pass
	}

	return os.Rename(tmpPath, t.filepath) == nil
}

func (t *globalPromptTracker) writeCompactedTempFile(f *os.File, entries []promptEntry) bool {
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return false
		}
	}
	return true
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

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close() }()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Sync()
}

// noOpPromptTracker is a fail-safe implementation that does nothing.
type noOpPromptTracker struct{}

func (n *noOpPromptTracker) Append(ctx context.Context, prompt string) error { return nil }
func (n *noOpPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	return nil, nil
}

// NewNoOpTracker returns a PromptTracker that performs no operations.
func NewNoOpTracker() ports.PromptTracker {
	return &noOpPromptTracker{}
}
