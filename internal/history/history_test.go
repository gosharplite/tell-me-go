// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/api"
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

	// Test function role (which follows model)
	if err := m.AddContent(api.Content{Role: "function", Parts: []api.Part{{Text: "result"}}}); err != nil {
		t.Errorf("failed to add function entry: %v", err)
	}

	// 2. Test Save and Load
	if err := m.Save(); err != nil {
		t.Fatalf("failed to save history: %v", err)
	}

	m2 := NewManager(historyFile)
	if err := m2.Load(); err != nil {
		t.Fatalf("failed to load history: %v", err)
	}

	if len(m2.GetContents()) != 3 {
		t.Errorf("expected 3 entries, got %d", len(m2.GetContents()))
	}

	// 3. Test Pruning
	m.Prune(1) // Should keep 2 messages
	if len(m.GetContents()) != 2 {
		t.Errorf("expected 2 entries after pruning, got %d", len(m.GetContents()))
	}
}
