// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

func TestHistoryManager_Basic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	m := NewManager(historyFile)

	// Test AddEntry and Alternation
	if err := m.AddEntry(genai.RoleModel, "fail"); err == nil {
		t.Error("expected error for first message being 'model'")
	}

	if err := m.AddEntry(genai.RoleUser, "Hello"); err != nil {
		t.Errorf("failed to add user entry: %v", err)
	}

	if err := m.AddEntry(genai.RoleUser, "Hello again"); err == nil {
		t.Error("expected error for consecutive 'user' roles")
	}

	if err := m.AddEntry(genai.RoleModel, "Hi there"); err != nil {
		t.Errorf("failed to add model entry: %v", err)
	}

	// Test Save and Load
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
}

func TestHistoryManager_Load_NonExistent(t *testing.T) {
	t.Parallel()
	m := NewManager("non-existent.json")
	if err := m.Load(); err != nil {
		t.Errorf("expected no error for non-existent file, got %v", err)
	}
	if len(m.Contents) != 0 {
		t.Errorf("expected empty contents, got %d", len(m.Contents))
	}
}

func TestHistoryManager_Load_Corrupted(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "corrupted.json")
	if err := os.WriteFile(historyFile, []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(historyFile)
	if err := m.Load(); err == nil {
		t.Error("expected error for corrupted JSON, got nil")
	}
}

func TestHistoryManager_Save_Error(t *testing.T) {
	t.Parallel()
	// Use a path that is impossible to create (e.g., child of a file)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "afile")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(filepath.Join(filePath, "history.json"))
	if err := m.Save(); err == nil {
		t.Error("expected error when directory creation fails, got nil")
	}
}

func TestHistoryManager_SnapshotRollback(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	m := NewManager(historyFile)

	_ = m.AddEntry(genai.RoleUser, "Initial")
	m.Snapshot()

	_ = m.AddEntry(genai.RoleModel, "Response")
	if len(m.GetContents()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m.GetContents()))
	}

	m.Rollback()
	if len(m.GetContents()) != 1 {
		t.Errorf("expected 1 entry after rollback, got %d", len(m.GetContents()))
	}
	if m.GetContents()[0].Parts[0].Text != "Initial" {
		t.Errorf("expected 'Initial', got '%s'", m.GetContents()[0].Parts[0].Text)
	}

	// Rollback with no snapshot should do nothing (or at least not crash)
	m3 := NewManager(filepath.Join(tmpDir, "m3.json"))
	m3.Rollback()
}

func TestHistoryManager_Prune(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "prune.json")
	m := NewManager(historyFile)

	// Setup: 6 messages (3 turns)
	_ = m.AddEntry("user", "U1")
	_ = m.AddEntry("model", "M1")
	_ = m.AddEntry("user", "U2")
	_ = m.AddEntry("model", "M2")
	_ = m.AddEntry("user", "U3")
	_ = m.AddEntry("model", "M3")

	tests := []struct {
		name     string
		maxTurns int
		wantLen  int
	}{
		{"Prune to 1 turn (drop to 1)", 1, 2},
		{"Prune to 2 turns (drop to 1)", 2, 2},
		{"Prune to more than exists", 10, 6},
		{"Prune to 0", 0, 6},
		{"Prune to negative", -1, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clone manager state for each test case using a unique file
			caseFile := filepath.Join(tmpDir, tt.name+".json")
			m2 := NewManager(caseFile)
			for _, c := range m.Contents {
				_ = m2.AddContent(&types.Content{Role: c.Role, Parts: c.Parts})
			}

			_, contents := m2.Prune(tt.maxTurns)
			if len(contents) != tt.wantLen {
				t.Errorf("Prune(%d) got %d messages, want %d", tt.maxTurns, len(contents), tt.wantLen)
			}
			if tt.wantLen > 0 && contents[0].Role != "user" {
				t.Errorf("Prune(%d) first message is not 'user'", tt.maxTurns)
			}
		})
	}
}

func TestHistoryManager_ReplaceRange(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "replace.json")
	m := NewManager(historyFile)

	// Setup: U1, M1, U2, M2
	_ = m.AddEntry("user", "U1")
	_ = m.AddEntry("model", "M1")
	_ = m.AddEntry("user", "U2")
	_ = m.AddEntry("model", "M2")

	// Scenario 1: Replace M1 with NewM1 (valid)
	newContents := []*types.Content{
		{Role: "model", Parts: []*types.Part{{Text: "NewM1"}}},
	}
	if err := m.ReplaceRange(1, 2, newContents); err != nil {
		t.Errorf("ReplaceRange valid failed: %v", err)
	}
	if m.Contents[1].Parts[0].Text != "NewM1" {
		t.Errorf("ReplaceRange content mismatch")
	}

	// Scenario 2: Invalid Range
	if err := m.ReplaceRange(-1, 0, nil); err == nil {
		t.Error("ReplaceRange expected error for invalid range")
	}

	// Scenario 3: Role Violation (Replace M1 with U_New)
	badContents := []*types.Content{
		{Role: "user", Parts: []*types.Part{{Text: "U_New"}}},
	}
	if err := m.ReplaceRange(1, 2, badContents); err == nil {
		t.Error("ReplaceRange expected error for role violation")
	}
}
