// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"path/filepath"
	"testing"

	"google.golang.org/genai"
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
	// Add more to test pruning
	_ = m.AddEntry("user", "Turn 2")
	_ = m.AddEntry("model", "Response 2")
	// [U1, M1, U2, M2] - 4 messages
	
	m.Prune(1) // Should keep last 1 turn (2 messages)
	if len(m.GetContents()) != 2 {
		t.Errorf("expected 2 entries after pruning, got %d", len(m.GetContents()))
	}
	if m.GetContents()[0].Role != genai.RoleUser {
		t.Error("first message after pruning is not 'user'")
	}
}

