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
	// 1. Define Paths
	globalDir := filepath.Join(homeDir, "output")
	trackerPath := filepath.Join(globalDir, "global_prompts.jsonl")

	// Legacy paths for migration
	legacyHiddenPath := filepath.Join(homeDir, ".tellmego", "prompts.jsonl")
	legacyRootPath := filepath.Join(homeDir, "global_prompts.jsonl")

	// 2. Ensure the output directory exists
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for prompt tracker: %w", err)
	}

	tracker := &globalPromptTracker{
		filepath: trackerPath,
	}

	// 3. Multi-Stage Migration Logic
	if _, err := os.Stat(trackerPath); os.IsNotExist(err) {
		var srcPath string

		// Check .tellmego/prompts.jsonl first
		if _, err := os.Stat(legacyHiddenPath); err == nil {
			srcPath = legacyHiddenPath
		} else if _, err := os.Stat(legacyRootPath); err == nil {
			// Check global_prompts.jsonl in root second
			srcPath = legacyRootPath
		}

		if srcPath != "" {
			// Perform robust migration (rename or copy+delete)
			if err := os.Rename(srcPath, trackerPath); err != nil {
				if copyErr := copyFile(srcPath, trackerPath); copyErr != nil {
					return tracker, fmt.Errorf("failed to migrate history file from %s: %w", srcPath, copyErr)
				}
				_ = os.Remove(srcPath)
			}

			// Cleanup the .tellmego folder if it's now empty
			if srcPath == legacyHiddenPath {
				_ = os.Remove(filepath.Dir(legacyHiddenPath))
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

	scanner := &reverseScanner{
		file: f,
		pos:  info.Size(),
	}

	seen := make(map[string]bool)
	result := make([]promptEntry, 0, limit)

	for scanner.pos > 0 && len(result) < limit {
		// Periodically check for context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lines, err := scanner.scanChunk()
		if err != nil {
			return nil, err
		}

		// Process lines in reverse order (most recent first)
		result = t.processReversedLines(lines, seen, result, limit)
	}

	return result, nil
}

type reverseScanner struct {
	file     *os.File
	pos      int64
	leftover []byte
}

func (s *reverseScanner) scanChunk() ([][]byte, error) {
	const chunkSize = 4096
	readSize := chunkSize
	if s.pos < int64(readSize) {
		readSize = int(s.pos)
	}
	s.pos -= int64(readSize)

	chunk := make([]byte, readSize)
	if _, err := s.file.ReadAt(chunk, s.pos); err != nil {
		return nil, fmt.Errorf("failed to read global prompts at %d: %w", s.pos, err)
	}

	// Combine with leftover from previous chunk
	data := append(chunk, s.leftover...)
	lines := bytes.Split(data, []byte{'\n'})

	if s.pos > 0 {
		s.leftover = lines[0]
		return lines[1:], nil
	}
	s.leftover = nil
	return lines, nil
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

func (t *globalPromptTracker) writeCompactedTempFile(w io.Writer, entries []promptEntry) bool {
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return false
		}
	}
	return true
}

func copyFile(src, dst string) (err error) {
	source, openErr := os.Open(src)
	if openErr != nil {
		return openErr
	}
	defer func() { _ = source.Close() }()

	destination, createErr := os.Create(dst)
	if createErr != nil {
		return createErr
	}

	// Capture Close error for the writable destination
	defer func() {
		closeErr := destination.Close()
		if err == nil {
			err = closeErr
		}
	}()

	if _, err = io.Copy(destination, source); err != nil {
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
