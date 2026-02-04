// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence and role alternation.
package history

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

// Manager handles loading, saving, and manipulating conversation history.
type Manager struct {
	mu       sync.RWMutex
	store    Store
	FilePath string
	Contents []*llm.Content
	backup   []*llm.Content // Keep a copy of the state before the current user prompt
}

// NewManager creates a new history manager for the given file path.
func NewManager(filePath string) *Manager {
	return &Manager{
		store:    NewJSONLStore(filePath),
		FilePath: filePath,
		Contents: []*llm.Content{},
	}
}

// SetStore allows injecting a custom store.
func (m *Manager) SetStore(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
}

// WithFileSystem sets the filesystem implementation for the default store.
func (m *Manager) WithFileSystem(fs fsutil.FileSystem) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.store.(*JSONLStore); ok {
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

	m.repairLocked()
	return nil
}

// repairLocked ensures the history is valid for the Gemini API after a crash.
// If the history ends with a model role containing function calls, it appends
// a user role with corresponding "interrupted" responses to maintain role alternation.
func (m *Manager) repairLocked() {
	if len(m.Contents) == 0 {
		return
	}

	last := m.Contents[len(m.Contents)-1]
	if last.Role != "model" {
		return
	}

	var responses []*llm.Part
	for _, p := range last.Parts {
		if p.FunctionCall != nil {
			responses = append(responses, &llm.Part{
				FunctionResponse: &llm.FunctionResponse{
					Name:     p.FunctionCall.Name,
					Response: map[string]interface{}{"result": "Error: System rebooted or session interrupted during tool execution. Results lost."},
				},
			})
		}
	}

	if len(responses) > 0 {
		m.Contents = append(m.Contents, &llm.Content{
			Role:  "user",
			Parts: responses,
		})
	}
}

// Save writes the current history to the file system atomically.
func (m *Manager) Save(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked(ctx)
}

func (m *Manager) saveLocked(ctx context.Context) error {
	m.cleanLocked()
	return m.store.Save(ctx, m.Contents)
}

func (m *Manager) cleanLocked() {
	// Clean up history: remove empty parts or parts with no content.
	// If a message would become empty, we add a placeholder to prevent API errors (400 INVALID_ARGUMENT).
	for _, content := range m.Contents {
		m.cleanContentLocked(content)
	}
}

func (m *Manager) cleanContentLocked(content *llm.Content) {
	var cleanParts []*llm.Part
	for _, p := range content.Parts {
		if p.Text == "" && p.InlineData == nil && p.FunctionCall == nil && p.FunctionResponse == nil && !p.Thought && len(p.ThoughtSignature) == 0 {
			continue
		}
		cleanParts = append(cleanParts, p)
	}
	if len(cleanParts) == 0 {
		cleanParts = append(cleanParts, &llm.Part{Text: "[empty response]"})
	}
	content.Parts = cleanParts
}

// AddEntry appends a new text message to the history.
func (m *Manager) AddEntry(ctx context.Context, role, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	content := &llm.Content{
		Role:  role,
		Parts: []*llm.Part{{Text: text}},
	}
	merged, err := m.addContentLocked(content)
	if err != nil {
		return err
	}
	if merged {
		return m.saveLocked(ctx)
	}
	m.cleanContentLocked(content)
	return m.store.Append(ctx, content)
}

// AddContent appends a full api.Content object to the history after validating role alternation.
func (m *Manager) AddContent(ctx context.Context, content *llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	merged, err := m.addContentLocked(content)
	if err != nil {
		return err
	}
	if merged {
		return m.saveLocked(ctx)
	}
	m.cleanContentLocked(content)
	return m.store.Append(ctx, content)
}

func (m *Manager) addContentLocked(content *llm.Content) (bool, error) {
	// 1. Validate role alternation
	if len(m.Contents) > 0 {
		last := m.Contents[len(m.Contents)-1]
		if last.Role == content.Role {
			// Merge parts instead of erroring to handle interrupted sessions or consecutive prompts
			last.Parts = append(last.Parts, content.Parts...)
			return true, nil
		}
	} else if content.Role != "user" {
		// First message must be user
		return false, fmt.Errorf("first message must be 'user', got '%s'", content.Role)
	}

	// 2. Add entry (Cloned to ensure no transient state persistence)
	m.Contents = append(m.Contents, m.clonePersistentContentLocked(content))

	return false, nil
}

// Snapshot takes a backup of the current state for potential rollback.
func (m *Manager) Snapshot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backup = make([]*llm.Content, len(m.Contents))
	copy(m.backup, m.Contents)
}

// Rollback restores the history to the state before Snapshot was called.
func (m *Manager) Rollback(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.backup != nil {
		m.Contents = m.backup
		_ = m.saveLocked(ctx) // Persist the rollback
	}
}

// Policy defines the limits for history management.
type Policy struct {
	MaxTurns int
}

// EnforcePolicy applies the given policy to the history.
func (m *Manager) EnforcePolicy(ctx context.Context, p Policy) int {
	pruned, _ := m.Prune(ctx, p.MaxTurns)
	return pruned
}

// Prune reduces the history when it exceeds maxTurns.
// Returns the number of turns removed and the new contents.
func (m *Manager) Prune(ctx context.Context, maxTurns int) (int, []*llm.Content) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxTurns <= 0 {
		return 0, m.contentsLocked()
	}
	maxMessages := maxTurns * 2
	if len(m.Contents) > maxMessages {
		// Prune down to 50% of the limit
		targetMessages := (maxTurns / 2) * 2
		if targetMessages < 2 {
			targetMessages = 2 // Keep at least one turn
		}

		removeCount := len(m.Contents) - targetMessages
		removeCount += (removeCount % 2) // Ensure role alternation

		if removeCount > 0 && removeCount < len(m.Contents) {
			m.Contents = m.Contents[removeCount:]
			_ = m.saveLocked(ctx) // Persist pruning
			return removeCount / 2, m.contentsLocked()
		}
	}
	return 0, m.contentsLocked()
}

func (m *Manager) contentsLocked() []*llm.Content {
	contents := make([]*llm.Content, len(m.Contents))
	copy(contents, m.Contents)
	return contents
}

// GetContents returns the current history contents.
func (m *Manager) GetContents() []*llm.Content {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to be safe?
	contents := make([]*llm.Content, len(m.Contents))
	copy(contents, m.Contents)
	return contents
}

// ReplaceRange replaces a range of history entries with new content.
// It ensures that role alternation is preserved if the caller provides alternating content.
func (m *Manager) ReplaceRange(ctx context.Context, start, end int, newContents []*llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if start < 0 || end > len(m.Contents) || start > end {
		return fmt.Errorf("invalid range: [%d, %d] for history length %d", start, end, len(m.Contents))
	}

	// 1. Create candidate slice
	head := m.Contents[:start]
	tail := m.Contents[end:]

	candidate := make([]*llm.Content, 0, len(head)+len(newContents)+len(tail))
	candidate = append(candidate, head...)
	candidate = append(candidate, newContents...)
	candidate = append(candidate, tail...)

	// 2. Validate role alternation for the entire candidate history
	for i := 1; i < len(candidate); i++ {
		if candidate[i].Role == candidate[i-1].Role {
			return fmt.Errorf("role alternation violation at index %d after replacement", i)
		}
	}

	// 3. Commit change (Cloned to ensure no transient state persistence)
	m.Contents = make([]*llm.Content, len(candidate))
	for i, c := range candidate {
		m.Contents[i] = m.clonePersistentContentLocked(c)
	}
	return m.saveLocked(ctx) // Persist replacement
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

// SetPinned toggles the pinned status of a turn (a pair of messages).
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

	// Save to disk if persistence is enabled
	return m.saveLocked(ctx)
}

// SetContents replaces the entire history and persists it to disk.
func (m *Manager) SetContents(ctx context.Context, contents []*llm.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Deepish copy to avoid "History Pollution" with transient state (like tool declarations)
	// that should only exist during a single API turn.
	newContents := make([]*llm.Content, len(contents))
	for i, c := range contents {
		newContents[i] = m.clonePersistentContentLocked(c)
	}

	m.Contents = newContents
	return m.saveLocked(ctx)
}

func (m *Manager) clonePersistentContentLocked(c *llm.Content) *llm.Content {
	if c == nil {
		return nil
	}
	return &llm.Content{
		Role:       c.Role,
		Parts:      append([]*llm.Part{}, c.Parts...),
		TokenCount: c.TokenCount,
		Pinned:     c.Pinned,
		// TransientParts are explicitly omitted to ensure they never reach memory/disk storage
	}
}
