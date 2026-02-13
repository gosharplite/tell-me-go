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

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// fileSnapshot represents a single file state in history.
type fileSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	Path      string    `json:"path"`
	Content   []byte    `json:"content"`
	Action    string    `json:"action"` // "WRITE" or "REPLACE"
}

// backupManager handles the snapshotting and restoration of files.
type backupManager struct {
	mu        sync.Mutex
	backups   []fileSnapshot
	maxStored int
	sm        domain_security.ISecurityManager
}

// newBackupManager creates a new backupManager.
func newBackupManager(sm domain_security.ISecurityManager, maxStored int) *backupManager {
	if maxStored <= 0 {
		maxStored = 10
	}
	return &backupManager{
		maxStored: maxStored,
		sm:        sm,
	}
}

// Snapshot records the current state of a file before it is modified.
func (b *backupManager) Snapshot(path string, action string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		// If file doesn't exist, we store nil content to represent "new file"
		content = nil
	}

	snap := fileSnapshot{
		Timestamp: time.Now(),
		Path:      absPath,
		Content:   content,
		Action:    action,
	}

	b.backups = append(b.backups, snap)
	if len(b.backups) > b.maxStored {
		b.backups = b.backups[1:]
	}
}

// Undo reverts the last N changes.
func (b *backupManager) Undo(ctx context.Context, n int) (string, error) {
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
		snap := b.backups[i]

		if _, err := b.sm.IsPathWritable(snap.Path); err != nil {
			return "", fmt.Errorf("permission denied for %s: %w", snap.Path, err)
		}

		if snap.Content == nil {
			// Original state was "not exists"
			if err := os.Remove(snap.Path); err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("failed to remove new file %s: %w", snap.Path, err)
			}
			results = append(results, fmt.Sprintf("Removed %s (was new file)", snap.Path))
		} else {
			if err := storage.AtomicWrite(ctx, snap.Path, snap.Content, 0644); err != nil {
				return "", fmt.Errorf("failed to restore %s: %w", snap.Path, err)
			}
			results = append(results, fmt.Sprintf("Restored %s", snap.Path))
		}

		count++
	}

	// Remove reverted snapshots
	b.backups = b.backups[:len(b.backups)-count]

	return fmt.Sprintf("Undo successful:\n%s", strings.Join(results, "\n")), nil
}
