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

	backfillCount := 0
	for _, c := range m.Contents {
		if c.ID == "" {
			c.ID = llm.NewID()
			backfillCount++
		}
	}
	if backfillCount > 0 {
		if saveErr := m.store.Save(ctx, m.Contents); saveErr != nil {
			return fmt.Errorf("failed to persist backfilled UUIDs: %w", saveErr)
		}
	}

	return nil
}

// Save writes the current history to the file system atomically.
func (m *Manager) Save(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.Save(ctx, m.Contents); err != nil {
		return err
	}
	return m.Sync(ctx)
}

// Sync ensures all buffered data is persisted to the physical disk.
func (m *Manager) Sync(ctx context.Context) error {
	return m.store.Sync(ctx)
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
	if err := m.store.Append(ctx, []*llm.Content{cloned}); err != nil {
		return err
	}
	m.Contents = append(m.Contents, cloned)
	return nil
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

	if err := m.store.Save(ctx, newContents); err != nil {
		return err
	}
	m.Contents = newContents
	return nil
}

func (m *Manager) clonePersistentContentLocked(c *llm.Content) *llm.Content {
	cloned := llm.CloneContent(c)
	if cloned != nil {
		cloned.TransientParts = nil
	}
	return cloned
}

// GetFilePath returns the file path of the history file.
func (m *Manager) GetFilePath() string {
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

// SetPinned toggles the pinned status of both messages in a turn,
// identified by the stable UUID of either message in the pair.
func (m *Manager) SetPinned(ctx context.Context, turnID string, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find the message with matching ID.
	i := -1
	for idx, c := range m.Contents {
		if c.ID == turnID {
			i = idx
			break
		}
	}
	if i == -1 {
		return fmt.Errorf("turn not found: %s", turnID)
	}

	// System messages are not part of any turn.
	if m.Contents[i].Role == "system" {
		return fmt.Errorf("turn not found: %s (system message)", turnID)
	}

	first, second, err := findTurnPair(m.Contents, i)
	if err != nil {
		return err
	}

	metadata := map[string]interface{}{"pinned": pinned}
	if err := m.store.UpdateMetadata(ctx, first, metadata); err != nil {
		return err
	}
	if err := m.store.UpdateMetadata(ctx, second, metadata); err != nil {
		return err
	}

	m.Contents[first].Pinned = pinned
	m.Contents[second].Pinned = pinned

	return nil
}

// findTurnPair determines the two indices forming the turn containing
// the message at index i. It handles normal user→model pairs and
// tool-call model→user (FunctionCall/FunctionResponse) pairs.
func findTurnPair(contents []*llm.Content, i int) (first, second int, err error) {
	role := contents[i].Role
	total := len(contents)

	if role == "model" && contentHasFunctionCall(contents[i]) {
		// Tool-call turn: model (FunctionCall) → user (FunctionResponse).
		if i+1 >= total || contents[i+1].Role != "user" || !contentHasFunctionResponse(contents[i+1]) {
			return 0, 0, fmt.Errorf("invalid turn pair: tool-call model at index %d has no function response", i)
		}
		return i, i + 1, nil
	}

	if role == "user" && contentHasFunctionResponse(contents[i]) {
		// Tool-call turn: user (FunctionResponse) → preceded by model (FunctionCall).
		if i-1 < 0 || contents[i-1].Role != "model" || !contentHasFunctionCall(contents[i-1]) {
			return 0, 0, fmt.Errorf("invalid turn pair: function response at index %d has no preceding function call", i)
		}
		return i - 1, i, nil
	}

	if role == "user" {
		// Normal turn: user → model.
		if i+1 >= total || contents[i+1].Role != "model" {
			return 0, 0, fmt.Errorf("invalid turn pair: user at index %d has no model response", i)
		}
		return i, i + 1, nil
	}

	if role == "model" {
		// Normal turn: model → preceded by user.
		if i-1 < 0 || contents[i-1].Role != "user" {
			return 0, 0, fmt.Errorf("invalid turn pair: model at index %d has no preceding user message", i)
		}
		return i - 1, i, nil
	}

	return 0, 0, fmt.Errorf("invalid role for turn pairing: %s", role)
}

// contentHasFunctionCall returns true if any part of the content contains a FunctionCall.
func contentHasFunctionCall(c *llm.Content) bool {
	for _, p := range c.Parts {
		if p.FunctionCall != nil {
			return true
		}
	}
	return false
}

// contentHasFunctionResponse returns true if any part of the content contains a FunctionResponse.
func contentHasFunctionResponse(c *llm.Content) bool {
	for _, p := range c.Parts {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

// addEntry appends a new text message to the history.
func (m *Manager) addEntry(ctx context.Context, role, text string) error {
	return m.AddContent(ctx, &llm.Content{
		Role:  role,
		ID:    llm.NewID(),
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

	if err := m.store.AppendParts(ctx, index, clonedParts); err != nil {
		return err
	}
	m.Contents[index].Parts = append(m.Contents[index].Parts, clonedParts...)
	return nil
}

// RollbackTurns removes the last N turns (1 turn = 2 messages) from the history.
// It returns the actual number of turns removed, the remaining turns, the remaining total messages, and any error.
func (m *Manager) RollbackTurns(ctx context.Context, turns int) (actualRemoved int, remainingTurns int, remainingMsgs int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	originalLen := len(m.Contents)
	hasSystem := originalLen > 0 && m.Contents[0].Role == "system"

	actualRemoved, newLen := calculateRollbackBounds(originalLen, turns, hasSystem)

	if newLen == originalLen && actualRemoved == 0 {
		return 0, rollbackRemainingTurns(originalLen, hasSystem), originalLen, nil
	}

	tempContents := m.Contents[:newLen]
	if err := m.commitRollback(ctx, tempContents, newLen, originalLen); err != nil {
		return 0, 0, 0, err
	}

	remainingMsgs = len(m.Contents)
	remainingTurns = rollbackRemainingTurns(remainingMsgs, hasSystem)

	return actualRemoved, remainingTurns, remainingMsgs, nil
}

func calculateRollbackBounds(originalLen int, turns int, hasSystem bool) (actualRemoved, newLen int) {
	if originalLen <= 0 {
		return 0, 0
	}

	offset := 0
	if hasSystem {
		offset = 1
	}
	effectiveLen := originalLen - offset

	if turns <= 0 {
		// Invariant: Rollback must result in an even number of effective messages (complete pairs).
		// If the initial state is odd (effective), we always drop the trailing partial turn even if turns=0.
		if effectiveLen%2 != 0 {
			return 0, originalLen - 1
		}
		return 0, originalLen
	}

	currentTurns := (effectiveLen + 1) / 2
	if turns >= currentTurns {
		return currentTurns, offset
	}

	// Calculate how many messages to drop.
	droppedMsgs := turns * 2
	if effectiveLen%2 != 0 {
		droppedMsgs -= 1
	}

	return turns, originalLen - droppedMsgs
}

// commitRollback persists the truncated contents and nils out removed entries.
// It follows durability-first semantics: persist succeeds before memory is mutated.
func (m *Manager) commitRollback(ctx context.Context, tempContents []*llm.Content, newLen, originalLen int) error {
	if err := m.store.Save(ctx, tempContents); err != nil {
		return fmt.Errorf("failed to persist rollback: %w", err)
	}

	// Persisted successfully, now safe to modify memory
	for i := newLen; i < originalLen; i++ {
		m.Contents[i] = nil
	}

	if newLen == 0 {
		m.Contents = nil
	} else {
		m.Contents = tempContents
	}
	return nil
}

// rollbackRemainingTurns computes the number of remaining turns after rollback.
func rollbackRemainingTurns(remainingMsgs int, hasSystem bool) int {
	effectiveLen := remainingMsgs
	if hasSystem {
		effectiveLen--
	}
	return effectiveLen / 2
}

// GetLastUserMessage finds the text of the last user message and the number of turns to rollback to remove it and everything after it.
func (m *Manager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastMsgText string
	humanMsgAbsoluteIndex := -1

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
				humanMsgAbsoluteIndex = i
				break
			}
		}
	}

	if humanMsgAbsoluteIndex == -1 {
		return "", 0, errors.New("no previous user message found to retry")
	}

	turnsToRollback := (total - humanMsgAbsoluteIndex + 1) / 2

	return lastMsgText, turnsToRollback, nil
}
