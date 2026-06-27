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

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
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
	fs                 persistence.FileSystem
	filepath           string
	compacting         atomic.Bool
	mu                 sync.RWMutex
	testCompactionHook func()
	wg                 sync.WaitGroup
}

// NewGlobalPromptTracker creates a new tracker pointing to the specified home directory.
func NewGlobalPromptTracker(fs persistence.FileSystem, homeDir string) (ports.PromptTracker, error) {
	// 1. Define Paths
	globalDir := filepath.Join(homeDir, "output")
	trackerPath := filepath.Join(globalDir, "global_prompts.jsonl")

	// Legacy paths for migration
	legacyHiddenPath := filepath.Join(homeDir, ".tellmego", "prompts.jsonl")
	legacyRootPath := filepath.Join(homeDir, "global_prompts.jsonl")

	// 2. Ensure the output directory exists
	ctx := context.Background()
	if err := fs.MkdirAll(ctx, globalDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for prompt tracker: %w", err)
	}

	tracker := &globalPromptTracker{
		fs:       fs,
		filepath: trackerPath,
	}

	// 3. Multi-Stage Migration Logic
	if _, err := fs.Stat(ctx, trackerPath); os.IsNotExist(err) {
		var srcPath string

		// Check .tellmego/prompts.jsonl first
		if _, err := fs.Stat(ctx, legacyHiddenPath); err == nil {
			srcPath = legacyHiddenPath
		} else if _, err := fs.Stat(ctx, legacyRootPath); err == nil {
			// Check global_prompts.jsonl in root second
			srcPath = legacyRootPath
		}

		if srcPath != "" {
			// Perform robust migration (rename or copy+delete)
			// Note: Rename is not part of FileSystem interface?
			// Wait, FileSystem interface does NOT have Rename.
			// I should use copy + remove if Rename is not available.
			// Actually, let's check FileSystem again.
			/*
				type FileSystem interface {
					ReadDir(ctx context.Context, name string) ([]os.DirEntry, error)
					ReadFile(ctx context.Context, name string) ([]byte, error)
					WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error
					AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error
					MkdirAll(ctx context.Context, path string, perm os.FileMode) error
					Stat(ctx context.Context, name string) (os.FileInfo, error)
					Open(ctx context.Context, name string) (File, error)
					OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (File, error)
					Remove(ctx context.Context, name string) error
					RemoveAll(ctx context.Context, path string) error
					Walk(ctx context.Context, root string, fn WalkFunc) error
				}
			*/
			// It doesn't have Rename.
			if err := copyFile(ctx, fs, srcPath, trackerPath); err != nil {
				return tracker, fmt.Errorf("failed to migrate history file from %s: %w", srcPath, err)
			}
			_ = fs.Remove(ctx, srcPath)

			// Cleanup the .tellmego folder if it's now empty
			if srcPath == legacyHiddenPath {
				_ = fs.Remove(ctx, filepath.Dir(legacyHiddenPath))
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

	// json.Marshal cannot fail for promptEntry (all fields are string).
	// The error path exists for interface contract compliance.
	// Coverage gap accepted by architect — structurally unreachable.
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal prompt entry: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	f, err := t.fs.OpenFile(ctx, t.filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open global prompts file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to append prompt: %w", err)
	}

	var size int64
	if info, err := t.fs.Stat(ctx, t.filepath); err == nil {
		size = info.Size()
	}

	// Trigger async compaction if file size exceeds threshold and no compaction is already in progress
	if size > compactionThresholdBytes {
		if t.compacting.CompareAndSwap(false, true) {
			t.wg.Add(1)
			go func() {
				defer t.wg.Done()
				t.compactLog(context.WithoutCancel(ctx))
			}()
		}
	}

	return nil
}

// Close ensures any ongoing compaction is finished or aborted.
func (t *globalPromptTracker) Close() error {
	t.wg.Wait()
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
	f, err := t.fs.Open(ctx, t.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open global prompts file: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := t.fs.Stat(ctx, t.filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat global prompts file: %w", err)
	}

	scanner := &reverseScanner{
		file: f,
		pos:  info.Size(),
	}

	seen := make(map[string]bool)
	result := make([]promptEntry, 0, limit)

	return t.readReverseEntries(ctx, scanner, seen, result, limit)
}

// readReverseEntries reads the file backwards in chunks, processing lines
// in reverse order, deduplicating, and collecting up to limit unique entries.
func (t *globalPromptTracker) readReverseEntries(ctx context.Context, scanner *reverseScanner, seen map[string]bool, result []promptEntry, limit int) ([]promptEntry, error) {
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
	file     persistence.File
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
		if !t.shouldStillCompact(ctx, &backoff) {
			return
		}
	}
}

// performCompactionPass attempts a single optimistic compaction pass.
// It returns true if successful, false if aborted due to concurrent writes or errors.
func (t *globalPromptTracker) performCompactionPass(ctx context.Context) bool {
	// Capture initial size for optimistic concurrency check
	info, err := t.fs.Stat(ctx, t.filepath)
	if err != nil {
		return false
	}

	data, err := t.prepareCompactedEntries(ctx)
	if err != nil {
		return false
	}
	if len(data) == 0 {
		return false
	}

	return t.optimisticWrite(ctx, data, info.Size())
}

// shouldStillCompact checks whether compaction should be retried after a
// failed pass. It verifies the file still exceeds the compaction threshold
// and applies exponential backoff before returning. Returns true if another
// compaction attempt is warranted.
func (t *globalPromptTracker) shouldStillCompact(ctx context.Context, backoff *time.Duration) bool {
	newInfo, err := t.fs.Stat(ctx, t.filepath)
	if err != nil {
		return false
	}
	if newInfo.Size() <= compactionThresholdBytes {
		return false
	}

	select {
	case <-ctx.Done():
		return false
	case <-time.After(*backoff):
		*backoff *= 2
		return true
	}
}

func (t *globalPromptTracker) writeCompactedData(w io.Writer, entries []promptEntry) bool {
	for _, entry := range entries {
		// json.Marshal cannot fail for promptEntry (all fields are string).
		// Returning false ensures callers handle this defensive path.
		// Coverage gap accepted by architect — structurally unreachable.
		data, err := json.Marshal(entry)
		if err != nil {
			return false // abort compaction; do not silently drop entries
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return false
		}
	}
	return true
}

// optimisticWrite atomically writes data to the tracker file only if the file
// size has not changed since initialSize was observed. It acquires the
// exclusive lock to serialize with concurrent Append calls and re-stats the
// file under the lock to detect races. Returns true on successful write.
func (t *globalPromptTracker) optimisticWrite(ctx context.Context, data []byte, initialSize int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, err := t.fs.Stat(ctx, t.filepath)
	if err != nil {
		return false
	}
	if info.Size() != initialSize {
		return false
	}

	return t.fs.AtomicWrite(ctx, t.filepath, data, 0644) == nil
}

// prepareCompactedEntries reads the global prompts file, deduplicates entries,
// reverses them into chronological order (oldest first), and serializes the
// result as JSONL bytes. It delegates to doLoadTopUniqueEntries for reading
// and writeCompactedData for serialization. The caller owns the returned byte
// slice. Returns nil, nil when no entries exist.
func (t *globalPromptTracker) prepareCompactedEntries(ctx context.Context) ([]byte, error) {
	entries, err := t.doLoadTopUniqueEntries(ctx, maxGlobalPrompts)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	// Reverse to chronological order (oldest first)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	var buf bytes.Buffer
	// writeCompactedData cannot return false when writing to bytes.Buffer:
	// json.Marshal on promptEntry never fails (all string fields), and
	// bytes.Buffer.Write never returns an error.
	// Coverage gap accepted by architect — structurally unreachable.
	if !t.writeCompactedData(&buf, entries) {
		return nil, fmt.Errorf("failed to serialize compacted entries")
	}

	return buf.Bytes(), nil
}

func copyFile(ctx context.Context, fs persistence.FileSystem, src, dst string) (err error) {
	data, err := fs.ReadFile(ctx, src)
	if err != nil {
		return err
	}
	return fs.AtomicWrite(ctx, dst, data, 0644)
}

// noOpPromptTracker is a fail-safe implementation that does nothing.
type noOpPromptTracker struct{}

func (n *noOpPromptTracker) Append(ctx context.Context, prompt string) error { return nil }
func (n *noOpPromptTracker) LoadTopN(ctx context.Context, limit int) ([]string, error) {
	return nil, nil
}
func (n *noOpPromptTracker) Close() error { return nil }

// NewNoOpTracker returns a PromptTracker that performs no operations.
func NewNoOpTracker() ports.PromptTracker {
	return &noOpPromptTracker{}
}
