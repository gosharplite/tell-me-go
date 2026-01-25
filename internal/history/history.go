// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package history manages conversation persistence and role alternation.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/api"
	"google.golang.org/genai"
)

// Manager handles loading, saving, and manipulating conversation history.
type Manager struct {
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
	dir := filepath.Dir(m.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create history directory: %w", err)
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
	return m.AddContent(&api.Content{
		Role:  role,
		Parts: []*api.Part{{Text: text}},
	})
}

// AddContent appends a full api.Content object to the history after validating role alternation.
func (m *Manager) AddContent(content *api.Content) error {
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
	m.backup = make([]*api.Content, len(m.Contents))
	copy(m.backup, m.Contents)
}

// Rollback restores the history to the state before Snapshot was called.
func (m *Manager) Rollback() {
	if m.backup != nil {
		m.Contents = m.backup
		m.Save() // Persist the rollback
	}
}

// Prune reduces the history to the last N turns (1 turn = 1 user + 1 model message).
func (m *Manager) Prune(maxTurns int) {
	if maxTurns <= 0 {
		return
	}
	maxMessages := maxTurns * 2
	if len(m.Contents) > maxMessages {
		removeCount := len(m.Contents) - maxMessages
		// Ensure the first message after pruning is 'user'.
		// Since role alternation is enforced, if the number of remaining messages
		// is even, the first one will be 'user' (assuming turns are [User, Model]).
		if (len(m.Contents)-removeCount)%2 != 0 {
			removeCount--
		}
		
		if removeCount > 0 {
			m.Contents = m.Contents[removeCount:]
		}
	}
}

// GetContents returns the current history contents.
func (m *Manager) GetContents() []*api.Content {
	return m.Contents
}

