// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"path/filepath"
	"testing"
)

func TestHistoryManager(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	m := NewManager(historyFile)

	// 1. Test AddEntry and Alternation
	if err := m.AddEntry("model", "fail"); err == nil {
		t.Error("expected error for first message being 'model'")
	}

	if err := m.AddEntry("user", "Hello"); err != nil {
		t.Errorf("failed to add user entry: %v", err)
	}

	if err := m.AddEntry("user", "Hello again"); err == nil {
		t.Error("expected error for consecutive 'user' roles")
	}

	if err := m.AddEntry("model", "Hi there"); err != nil {
		t.Errorf("failed to add model entry: %v", err)
	}

	// 2. Test Save and Load
	if err := m.Save(); err != nil {
		t.Fatalf("failed to save history: %v", err)
	}

	m2 := NewManager(historyFile)
	if err := m2.Load(); err != nil {
		t.Fatalf("failed to load history: %v", err)
	}

	if len(m2.GetContents()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m2.GetContents()))
	}

	// 3. Test Pruning
	// Add more entries
	m.AddEntry("user", "3")
	m.AddEntry("model", "4")
	m.AddEntry("user", "5")
	m.AddEntry("model", "6") // Total 6 messages (3 turns)

	m.Prune(2) // Should keep 4 messages (2 turns)
	if len(m.GetContents()) != 4 {
		t.Errorf("expected 4 entries after pruning, got %d", len(m.GetContents()))
	}
	if m.GetContents()[0].Parts[0].Text != "3" {
		t.Errorf("expected first message after pruning to be '3', got '%s'", m.GetContents()[0].Parts[0].Text)
	}
}
