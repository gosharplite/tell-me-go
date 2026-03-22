// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence.
package history

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Manager handles loading, saving, and append-only log compaction of conversation history,
// ensuring O(1) in-memory state scaling and efficient persistent log rotation.
type Manager struct {
	mu       sync.RWMutex
	store    store
	FilePath string
	Contents []*llm.Content
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
		if errors.Is(err, ports.ErrHistoryNotFound) {
			m.Contents = []*llm.Content{}
			return nil
		}
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

// RollbackTurns removes the last N turns (1 turn = 2 messages) from the history.
// It returns the actual number of turns removed, the remaining turns, the remaining total messages, and any error.
func (m *Manager) RollbackTurns(ctx context.Context, turns int) (actualRemoved int, remainingTurns int, remainingMsgs int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	originalLen := len(m.Contents)
	if originalLen == 0 || turns <= 0 {
		return 0, originalLen / 2, originalLen, nil
	}

	originalContents := m.Contents

	// Prevent overflow and handle out-of-bounds turns
	if turns > originalLen/2 {
		actualRemoved = originalLen / 2
		m.Contents = nil
	} else {
		actualRemoved = turns
		newLen := originalLen - (turns * 2)

		// Nil out the truncated pointers to prevent memory leaks
		for i := newLen; i < originalLen; i++ {
			m.Contents[i] = nil
		}

		m.Contents = m.Contents[:newLen]
	}

	if err := m.store.Save(ctx, m.Contents); err != nil {
		// Rollback in-memory state on I/O failure to maintain atomicity
		m.Contents = originalContents
		return 0, 0, 0, fmt.Errorf("failed to persist rollback: %w", err)
	}

	remainingMsgs = len(m.Contents)
	remainingTurns = remainingMsgs / 2

	return actualRemoved, remainingTurns, remainingMsgs, nil
}

// GetLastUserMessage finds the text of the last user message and the number of turns to rollback to remove it and everything after it.
func (m *Manager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastMsgText string
	var humanTurnIndex int
	
	total := len(m.Contents)
	
	for i := total - 1; i >= 0; i-- {
		if m.Contents[i].Role == "user" {
			var textBuilder string
			for _, part := range m.Contents[i].Parts {
				if part.Text != "" {
					textBuilder += part.Text
				}
			}
			
			if textBuilder != "" {
				lastMsgText = textBuilder
				humanTurnIndex = i / 2 
				break
			}
		}
	}

	if lastMsgText == "" {
		return "", 0, errors.New("no previous user message found to retry")
	}

	totalTurns := total / 2
	turnsToRollback := totalTurns - humanTurnIndex

	return lastMsgText, turnsToRollback, nil
}
