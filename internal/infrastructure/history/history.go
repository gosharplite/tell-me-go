// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence.
package history

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// Manager handles loading, saving, and append-only log compaction of conversation history,
// ensuring O(1) in-memory state scaling and efficient persistent log rotation.
type Manager struct {
	mu       sync.RWMutex
	store    store
	FilePath string
	Contents []*llm.Content
	backup   []*llm.Content // Keep a copy of the state before the current user prompt
}

// NewManager creates a new history manager for the given file path.
func NewManager(fs persistence.FileSystem, filePath string, archivePath string) *Manager {
	return &Manager{
		store:    newJSONLStore(fs, filePath, archivePath),
		FilePath: filePath,
		Contents: []*llm.Content{},
	}
}

// setStore allows injecting a custom store.
func (m *Manager) setStore(s store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = s
}

// withFileSystem sets the filesystem implementation for the default store.
func (m *Manager) withFileSystem(fs persistence.FileSystem) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.store.(*jsonlStore); ok {
		s.withFileSystem(fs)
	}
	return m
}

// Load reads the history from the file system.
func (m *Manager) Load(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	contents, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	m.Contents = contents
	return nil
}

// Save writes the current history to the file system atomically.
func (m *Manager) Save(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Save(ctx, m.Contents)
}

// Archive appends content entries to the archive file.
func (m *Manager) Archive(ctx context.Context, contents []*llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Archive(ctx, contents)
}

// AddContent appends a full api.Content object to the history.
// Note: It does NOT validate role alternation or clean content;
// these are responsibilities of the Orchestration layer.
func (m *Manager) AddContent(ctx context.Context, content *llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cloned := m.clonePersistentContentLocked(content)
	m.Contents = append(m.Contents, cloned)
	return m.store.Append(ctx, []*llm.Content{cloned})
}

// GetTotalEntries returns the total number of content entries currently stored.
func (m *Manager) GetTotalEntries() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Contents)
}

// GetWindow returns a deep copy of a specific range of history.
// If endIdx is -1, it returns from startIdx to the end of the history.
func (m *Manager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.Contents)

	if startIdx < 0 {
		return nil, fmt.Errorf("invalid startIdx: %d", startIdx)
	}
	if startIdx > total {
		startIdx = total
	}

	if endIdx == -1 || endIdx > total {
		endIdx = total
	}
	if endIdx < startIdx {
		return nil, fmt.Errorf("invalid range: startIdx=%d, endIdx=%d", startIdx, endIdx)
	}

	window := m.Contents[startIdx:endIdx]
	cloned := make([]*llm.Content, len(window))
	for i, c := range window {
		cloned[i] = llm.CloneContent(c)
	}

	return cloned, nil
}

// SetContents replaces the entire history and persists it to disk.
func (m *Manager) SetContents(ctx context.Context, contents []*llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newContents := make([]*llm.Content, len(contents))
	for i, c := range contents {
		newContents[i] = m.clonePersistentContentLocked(c)
	}

	m.Contents = newContents
	return m.store.Save(ctx, m.Contents)
}

func (m *Manager) clonePersistentContentLocked(c *llm.Content) *llm.Content {
	cloned := llm.CloneContent(c)
	if cloned != nil {
		cloned.TransientParts = nil
	}
	return cloned
}

// getPath returns the file path of the history file.
func (m *Manager) getPath() string {
	return m.FilePath
}

// GetResolver returns an AssetResolver from the underlying store.
func (m *Manager) GetResolver() llm.AssetResolver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.store.(llm.AssetResolver); ok {
		return r
	}
	return nil
}

// SetPinned toggles the pinned status of messages in a turn.
func (m *Manager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Turns are pairs. Turn 0 is messages 0 and 1.
	startIdx := turnIndex * 2
	if startIdx < 0 || startIdx+1 >= len(m.Contents) {
		return fmt.Errorf("invalid turn index: %d (history length: %d)", turnIndex, len(m.Contents))
	}

	metadata := map[string]interface{}{"pinned": pinned}
	if err := m.store.UpdateMetadata(ctx, startIdx, metadata); err != nil {
		return err
	}
	if err := m.store.UpdateMetadata(ctx, startIdx+1, metadata); err != nil {
		return err
	}

	m.Contents[startIdx].Pinned = pinned
	m.Contents[startIdx+1].Pinned = pinned

	return nil
}

// snapshot takes a backup of the current state for potential rollback.
func (m *Manager) snapshot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backup = make([]*llm.Content, len(m.Contents))
	for i, c := range m.Contents {
		m.backup[i] = llm.CloneContent(c)
	}
}

// rollback restores the history to the state before Snapshot was called.
func (m *Manager) rollback(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.backup != nil {
		m.Contents = m.backup
		if err := m.store.Truncate(ctx, len(m.backup)); err != nil {
			log.Printf("Warning: failed to persist history rollback truncation: %v", err)
		}
	}
}

// addEntry appends a new text message to the history.
func (m *Manager) addEntry(ctx context.Context, role, text string) error {
	return m.AddContent(ctx, &llm.Content{
		Role:  role,
		Parts: []*llm.Part{{Text: text}},
	})
}

// AppendParts appends parts to an existing content entry at the specified index.
func (m *Manager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 || index >= len(m.Contents) {
		return fmt.Errorf("invalid index: %d", index)
	}

	clonedParts := make([]*llm.Part, len(parts))
	for i, p := range parts {
		dummy := llm.CloneContent(&llm.Content{Parts: []*llm.Part{p}})
		clonedParts[i] = dummy.Parts[0]
	}

	m.Contents[index].Parts = append(m.Contents[index].Parts, clonedParts...)
	return m.store.AppendParts(ctx, index, clonedParts)
}
