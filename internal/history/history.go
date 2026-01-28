// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence and role alternation.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/api"
	"google.golang.org/genai"
)

// Manager handles loading, saving, and manipulating conversation history.
type Manager struct {
	mu       sync.RWMutex
	FilePath string
	Contents []*api.Content
	backup   []*api.Content // Keep a copy of the state before the current user prompt
}

// NewManager creates a new history manager for the given file path.
func NewManager(filePath string) *Manager {
	return &Manager{
		FilePath: filePath,
		Contents: []*api.Content{},
	}
}

// Load reads the history from the file system.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(m.FilePath); os.IsNotExist(err) {
		m.Contents = []*api.Content{}
		return nil
	}

	data, err := os.ReadFile(m.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read history file: %w", err)
	}

	if err := json.Unmarshal(data, &m.Contents); err != nil {
		return fmt.Errorf("failed to parse history JSON: %w", err)
	}

	return nil
}

// Save writes the current history to the file system atomically.
func (m *Manager) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked()
}

func (m *Manager) saveLocked() error {
	dir := filepath.Dir(m.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create history directory: %w", err)
	}

	// Clean up history: remove empty parts or parts with no content.
	// If a message would become empty, we add a placeholder to prevent API errors (400 INVALID_ARGUMENT).
	for _, content := range m.Contents {
		var cleanParts []*api.Part
		for _, p := range content.Parts {
			if p.Text == "" && p.InlineData == nil && p.FunctionCall == nil && p.FunctionResponse == nil && !p.Thought {
				continue
			}
			cleanParts = append(cleanParts, p)
		}
		if len(cleanParts) == 0 {
			cleanParts = append(cleanParts, &api.Part{Text: "[empty response]"})
		}
		content.Parts = cleanParts
	}

	data, err := json.MarshalIndent(m.Contents, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	tmpFile := m.FilePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp history file: %w", err)
	}

	if err := os.Rename(tmpFile, m.FilePath); err != nil {
		return fmt.Errorf("failed to rename history file: %w", err)
	}

	return nil
}

// AddEntry appends a new text message to the history.
func (m *Manager) AddEntry(role, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addContentLocked(&api.Content{
		Role:  role,
		Parts: []*api.Part{{Text: text}},
	})
}

// AddContent appends a full api.Content object to the history after validating role alternation.
func (m *Manager) AddContent(content *api.Content) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addContentLocked(content)
}

func (m *Manager) addContentLocked(content *api.Content) error {
	// 1. Validate role alternation
	if len(m.Contents) > 0 {
		lastRole := m.Contents[len(m.Contents)-1].Role
		if lastRole == content.Role {
			return fmt.Errorf("role alternation violation: last role was %s, cannot add another %s", lastRole, content.Role)
		}
	} else if content.Role != genai.RoleUser {
		// First message must be user
		return fmt.Errorf("first message must be 'user', got '%s'", content.Role)
	}

	// 2. Add entry
	m.Contents = append(m.Contents, content)

	return nil
}

// Snapshot takes a backup of the current state for potential rollback.
func (m *Manager) Snapshot() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backup = make([]*api.Content, len(m.Contents))
	copy(m.backup, m.Contents)
}

// Rollback restores the history to the state before Snapshot was called.
func (m *Manager) Rollback() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.backup != nil {
		m.Contents = m.backup
		m.saveLocked() // Persist the rollback
	}
}

// Prune reduces the history when it exceeds maxTurns.
// To improve cache efficiency, it doesn't just prune 1 turn;
// it prunes down to 50% of maxTurns to allow for a stable cache prefix
// during the next 50% of the conversation.
// Returns the number of turns removed.
func (m *Manager) Prune(maxTurns int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxTurns <= 0 {
		return 0
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
			return removeCount / 2
		}
	}
	return 0
}

// GetContents returns the current history contents.
func (m *Manager) GetContents() []*api.Content {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to be safe?
	contents := make([]*api.Content, len(m.Contents))
	copy(contents, m.Contents)
	return contents
}

// GetPath returns the file path of the history file.
func (m *Manager) GetPath() string {
	return m.FilePath
}
