// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// fileSnapshot represents a single file state in history.
type fileSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	Path      string    `json:"path"`
	Content   []byte    `json:"content"`
	Action    string    `json:"action"` // "WRITE", "REPLACE", "APPEND", or "DELETE"
}

// backupManager handles the snapshotting and restoration of files.
type backupManager struct {
	mu        sync.Mutex
	backups   []fileSnapshot
	maxStored int
	sm        domain_security.PathValidator
	fs        domain_persistence.FileSystem
}

// newBackupManager creates a new backupManager.
func newBackupManager(sm domain_security.PathValidator, fs domain_persistence.FileSystem, maxStored int) *backupManager {
	if maxStored <= 0 {
		maxStored = 10
	}
	return &backupManager{
		maxStored: maxStored,
		sm:        sm,
		fs:        fs,
	}
}

// snapshot records the current state of a file before it is modified.
func (b *backupManager) snapshot(ctx context.Context, path string, action string) error {
	// Resolve path and read file content OUTSIDE the critical section.
	// Holding the mutex across synchronous disk I/O would cause thread
	// starvation for concurrent callers.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("snapshot: resolve path %s: %w", path, err)
	}

	content, err := b.fs.ReadFile(ctx, absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Genuinely new file — store nil content to represent "new file"
			content = nil
		} else {
			return fmt.Errorf("snapshot: read %s: %w", absPath, err)
		}
	}

	snap := fileSnapshot{
		Timestamp: time.Now(),
		Path:      absPath,
		Content:   content,
		Action:    action,
	}

	// Lock ONLY for the in-memory state mutation.
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backups = append(b.backups, snap)
	if len(b.backups) > b.maxStored {
		b.backups = b.backups[1:]
	}
	return nil
}

// undo reverts the last N changes.
func (b *backupManager) undo(ctx context.Context, n int) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.backups) == 0 {
		return "No snapshots available to undo.", nil
	}

	if n <= 0 {
		n = 1
	}

	var results []string
	count := 0
	for i := len(b.backups) - 1; i >= 0 && count < n; i-- {
		msg, err := b.undoOne(ctx, b.backups[i])
		if err != nil {
			return "", err
		}
		results = append(results, msg)
		count++
	}

	// Remove reverted snapshots
	b.backups = b.backups[:len(b.backups)-count]

	return fmt.Sprintf("Undo successful:\n%s", strings.Join(results, "\n")), nil
}

// undoOne restores or removes the file described by a single snapshot.
func (b *backupManager) undoOne(ctx context.Context, snap fileSnapshot) (string, error) {
	if _, err := b.sm.IsPathWritable(snap.Path); err != nil {
		return "", fmt.Errorf("permission denied for %s: %w", snap.Path, err)
	}

	if snap.Content == nil {
		// Original state was "not exists"
		if err := b.fs.Remove(ctx, snap.Path); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to remove new file %s: %w", snap.Path, err)
		}
		return fmt.Sprintf("Removed %s (was new file)", snap.Path), nil
	}

	if err := b.fs.AtomicWrite(ctx, snap.Path, snap.Content, 0644); err != nil {
		return "", fmt.Errorf("failed to restore %s: %w", snap.Path, err)
	}
	return fmt.Sprintf("Restored %s", snap.Path), nil
}
