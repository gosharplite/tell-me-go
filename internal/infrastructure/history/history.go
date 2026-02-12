// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence.
package history

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// Manager handles loading, saving, and basic manipulation of conversation history.
// It acts as a dumb persistence adapter with an in-memory cache.
type Manager struct {
	mu       sync.RWMutex
	store    store
	FilePath string
	Contents []*llm.Content
	backup   []*llm.Content // Keep a copy of the state before the current user prompt
}

// NewManager creates a new history manager for the given file path.
func NewManager(filePath string) *Manager {
	return &Manager{
		store:    newJSONLStore(filePath),
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

// WithFileSystem sets the filesystem implementation for the default store.
func (m *Manager) WithFileSystem(fs storage.FileSystem) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.store.(*jsonlStore); ok {
		s.WithFileSystem(fs)
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

// GetContents returns a copy of the current history contents.
func (m *Manager) GetContents() []*llm.Content {
	m.mu.RLock()
	defer m.mu.RUnlock()
	contents := make([]*llm.Content, len(m.Contents))
	for i, c := range m.Contents {
		contents[i] = llm.CloneContent(c)
	}
	return contents
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

// GetPath returns the file path of the history file.
func (m *Manager) GetPath() string {
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

	m.Contents[startIdx].Pinned = pinned
	m.Contents[startIdx+1].Pinned = pinned

	return m.store.Save(ctx, m.Contents)
}

// Snapshot takes a backup of the current state for potential rollback.
func (m *Manager) Snapshot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backup = make([]*llm.Content, len(m.Contents))
	for i, c := range m.Contents {
		m.backup[i] = llm.CloneContent(c)
	}
}

// Rollback restores the history to the state before Snapshot was called.
func (m *Manager) Rollback(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.backup != nil {
		m.Contents = m.backup
		_ = m.store.Save(ctx, m.Contents) // Persist the rollback
	}
}

// AddEntry appends a new text message to the history.
func (m *Manager) AddEntry(ctx context.Context, role, text string) error {
	return m.AddContent(ctx, &llm.Content{
		Role:  role,
		Parts: []*llm.Part{{Text: text}},
	})
}
