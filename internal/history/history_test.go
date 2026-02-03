// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"google.golang.org/genai"
)

func TestHistoryManager_Basic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Test AddEntry and Alternation
	if err := m.AddEntry(ctx, genai.RoleModel, "fail"); err == nil {
		t.Error("expected error for first message being 'model'")
	}

	if err := m.AddEntry(ctx, genai.RoleUser, "Hello"); err != nil {
		t.Errorf("failed to add user entry: %v", err)
	}

	if err := m.AddEntry(ctx, genai.RoleUser, "Hello again"); err != nil {
		t.Errorf("failed to add consecutive user entry: %v", err)
	}

	if err := m.AddEntry(ctx, genai.RoleModel, "Hi there"); err != nil {
		t.Errorf("failed to add model entry: %v", err)
	}

	// Test Save and Load
	if err := m.Save(ctx); err != nil {
		t.Fatalf("failed to save history: %v", err)
	}

	m2 := NewManager(historyFile)
	if err := m2.Load(ctx); err != nil {
		t.Fatalf("failed to load history: %v", err)
	}

	if len(m2.GetContents()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m2.GetContents()))
	}
}

func TestHistoryManager_Load_NonExistent(t *testing.T) {
	t.Parallel()
	m := NewManager("non-existent.json")
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
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
	ctx := context.Background()
	if err := m.Load(ctx); err == nil {
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
	ctx := context.Background()
	if err := m.Save(ctx); err == nil {
		t.Error("expected error when directory creation fails, got nil")
	}
}

func TestHistoryManager_SnapshotRollback(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	_ = m.AddEntry(ctx, genai.RoleUser, "Initial")
	m.Snapshot()

	_ = m.AddEntry(ctx, genai.RoleModel, "Response")
	if len(m.GetContents()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m.GetContents()))
	}

	m.Rollback(ctx)
	if len(m.GetContents()) != 1 {
		t.Errorf("expected 1 entry after rollback, got %d", len(m.GetContents()))
	}
	if m.GetContents()[0].Parts[0].Text != "Initial" {
		t.Errorf("expected 'Initial', got '%s'", m.GetContents()[0].Parts[0].Text)
	}

	// Rollback with no snapshot should do nothing (or at least not crash)
	m3 := NewManager(filepath.Join(tmpDir, "m3.json"))
	m3.Rollback(ctx)
}

func TestHistoryManager_Prune(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "prune.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Setup: 6 messages (3 turns)
	_ = m.AddEntry(ctx, "user", "U1")
	_ = m.AddEntry(ctx, "model", "M1")
	_ = m.AddEntry(ctx, "user", "U2")
	_ = m.AddEntry(ctx, "model", "M2")
	_ = m.AddEntry(ctx, "user", "U3")
	_ = m.AddEntry(ctx, "model", "M3")

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
				_ = m2.AddContent(ctx, &llm.Content{Role: c.Role, Parts: c.Parts})
			}

			_, contents := m2.Prune(ctx, tt.maxTurns)
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
	ctx := context.Background()

	// Setup: U1, M1, U2, M2
	_ = m.AddEntry(ctx, "user", "U1")
	_ = m.AddEntry(ctx, "model", "M1")
	_ = m.AddEntry(ctx, "user", "U2")
	_ = m.AddEntry(ctx, "model", "M2")

	// Scenario 1: Replace M1 with NewM1 (valid)
	newContents := []*llm.Content{
		{Role: "model", Parts: []*llm.Part{{Text: "NewM1"}}},
	}
	if err := m.ReplaceRange(ctx, 1, 2, newContents); err != nil {
		t.Errorf("ReplaceRange valid failed: %v", err)
	}
	if m.Contents[1].Parts[0].Text != "NewM1" {
		t.Errorf("ReplaceRange content mismatch")
	}

	// Scenario 2: Invalid Range
	if err := m.ReplaceRange(ctx, -1, 0, nil); err == nil {
		t.Error("ReplaceRange expected error for invalid range")
	}

	// Scenario 3: Role Violation (Replace M1 with U_New)
	badContents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "U_New"}}},
	}
	if err := m.ReplaceRange(ctx, 1, 2, badContents); err == nil {
		t.Error("ReplaceRange expected error for role violation")
	}
}

func TestHistoryManager_RepairInterruptedTool(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "repair.json")
	ctx := context.Background()

	// Create a history that ends with a model call that has a function call
	m := NewManager(historyFile)
	m.Contents = append(m.Contents, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "call tool"}},
	})
	m.Contents = append(m.Contents, &llm.Content{
		Role: "model",
		Parts: []*llm.Part{{
			FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"q": "123"}},
		}},
	})
	_ = m.Save(ctx)

	// Reload - this should trigger repairLocked
	m2 := NewManager(historyFile)
	if err := m2.Load(ctx); err != nil {
		t.Fatal(err)
	}

	contents := m2.GetContents()
	if len(contents) != 3 {
		t.Fatalf("expected 3 messages after repair, got %d", len(contents))
	}
	last := contents[2]
	if last.Role != "user" {
		t.Errorf("repaired message role should be 'user', got %s", last.Role)
	}
	if last.Parts[0].FunctionResponse == nil {
		t.Error("repaired message should contain a FunctionResponse")
	}
}

func TestHistoryManager_EnforcePolicy(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "policy.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Add 3 turns (6 messages)
	for i := 1; i <= 3; i++ {
		_ = m.AddEntry(ctx, "user", fmt.Sprintf("U%d", i))
		_ = m.AddEntry(ctx, "model", fmt.Sprintf("M%d", i))
	}

	// Enforce policy: MaxTurns = 1 (should prune to 1 turn = 2 messages)
	pruned := m.EnforcePolicy(ctx, Policy{MaxTurns: 1})
	if pruned == 0 {
		t.Error("expected turns to be pruned")
	}

	if len(m.GetContents()) != 2 {
		t.Errorf("expected 2 messages after EnforcePolicy, got %d", len(m.GetContents()))
	}
}

func TestHistoryManager_CleanContent(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "clean.json")
	m := NewManager(historyFile)
	// ctx := context.Background()

	tests := []struct {
		name     string
		content  *llm.Content
		wantText string
		wantLen  int
	}{
		{
			name: "remove empty parts",
			content: &llm.Content{
				Role: "user",
				Parts: []*llm.Part{
					{Text: "hello"},
					{Text: ""},
					{Text: "world"},
				},
			},
			wantLen: 2,
		},
		{
			name: "handle empty message",
			content: &llm.Content{
				Role: "model",
				Parts: []*llm.Part{
					{Text: ""},
				},
			},
			wantText: "[empty response]",
			wantLen:  1,
		},
		{
			name: "preserve thoughts",
			content: &llm.Content{
				Role: "model",
				Parts: []*llm.Part{
					{Thought: true},
					{ThoughtSignature: []byte("sig")},
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.mu.Lock()
			m.cleanContentLocked(tt.content)
			m.mu.Unlock()

			if len(tt.content.Parts) != tt.wantLen {
				t.Errorf("got %d parts, want %d", len(tt.content.Parts), tt.wantLen)
			}
			if tt.wantText != "" && tt.content.Parts[0].Text != tt.wantText {
				t.Errorf("got text %q, want %q", tt.content.Parts[0].Text, tt.wantText)
			}
		})
	}
}

func TestHistoryManager_Interfaces(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "interfaces.json")
	m := NewManager(historyFile)

	if m.GetPath() != historyFile {
		t.Errorf("GetPath() = %s, want %s", m.GetPath(), historyFile)
	}

	if m.GetResolver() == nil {
		t.Error("GetResolver() should not be nil for JSONLStore")
	}

	fs := &fsutil.OSFileSystem{}
	m.WithFileSystem(fs)
	// Verify it reached the store
	if s, ok := m.store.(*JSONLStore); ok {
		if s.fs != fs {
			t.Error("WithFileSystem did not propagate to store")
		}
	} else {
		t.Error("store is not JSONLStore")
	}

	m.SetStore(m.store) // Coverage for SetStore
}

func TestHistoryManager_Repair_NoTool(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "repair_no_tool.json")
	m := NewManager(historyFile)
	m.Contents = append(m.Contents, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}})
	m.Contents = append(m.Contents, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hello"}}})
	m.repairLocked()
	if len(m.Contents) != 2 {
		t.Errorf("expected 2 messages, got %d", len(m.Contents))
	}
}

type mockStore struct{}

func (s *mockStore) Load(ctx context.Context) ([]*llm.Content, error)        { return nil, nil }
func (s *mockStore) Save(ctx context.Context, contents []*llm.Content) error { return nil }
func (s *mockStore) Append(ctx context.Context, content *llm.Content) error  { return nil }

func TestHistoryManager_GetResolver_Nil(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "history.json"))
	m.SetStore(&mockStore{})
	if m.GetResolver() != nil {
		t.Error("expected nil resolver for mockStore")
	}
}

func TestHistoryManager_Repair_Empty(t *testing.T) {
	m := &Manager{}
	m.repairLocked()
	if len(m.Contents) != 0 {
		t.Error("expected empty contents")
	}
}

func TestHistoryManager_AddContent_Merging(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "history.json"))
	ctx := context.Background()

	// 1. First message not user
	err := m.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Hi"}}})
	if err == nil {
		t.Error("expected error for first message not being user")
	}

	// 2. Role alternation merging
	_ = m.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U1"}}})
	err = m.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "U2"}}})
	if err != nil {
		t.Errorf("expected merging, got error: %v", err)
	}

	contents := m.GetContents()
	if len(contents) != 1 {
		t.Errorf("expected 1 message after merging, got %d", len(contents))
	}
	if len(contents[0].Parts) != 2 {
		t.Errorf("expected 2 parts in merged message, got %d", len(contents[0].Parts))
	}
}

func TestHistoryManager_FileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "new_subdir", "history.jsonL")

	h := NewManager(historyFile)
	ctx := context.Background()

	// Add an entry to trigger a save
	err := h.AddEntry(ctx, "user", "test message")
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(historyFile); os.IsNotExist(err) {
		t.Errorf("expected history file to be created at %s", historyFile)
	}
}

func TestHistoryManager_SetPinned(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "pin.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Setup: 2 turns (4 messages)
	_ = m.AddEntry(ctx, "user", "U1")
	_ = m.AddEntry(ctx, "model", "M1")
	_ = m.AddEntry(ctx, "user", "U2")
	_ = m.AddEntry(ctx, "model", "M2")

	// Pin Turn 0
	if err := m.SetPinned(ctx, 0, true); err != nil {
		t.Fatalf("SetPinned(0, true) failed: %v", err)
	}

	contents := m.GetContents()
	if !contents[0].Pinned || !contents[1].Pinned {
		t.Error("Turn 0 (messages 0 and 1) should be pinned")
	}
	if contents[2].Pinned || contents[3].Pinned {
		t.Error("Turn 1 should not be pinned")
	}

	// Unpin Turn 0
	if err := m.SetPinned(ctx, 0, false); err != nil {
		t.Fatalf("SetPinned(0, false) failed: %v", err)
	}
	contents = m.GetContents()
	if contents[0].Pinned || contents[1].Pinned {
		t.Error("Turn 0 should be unpinned")
	}

	// Invalid Index
	if err := m.SetPinned(ctx, 2, true); err == nil {
		t.Error("expected error for invalid turn index")
	}
	if err := m.SetPinned(ctx, -1, true); err == nil {
		t.Error("expected error for negative turn index")
	}
}
