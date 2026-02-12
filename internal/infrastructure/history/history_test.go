// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestHistoryManager_Basic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Test AddEntry - Note: Dumb manager does not validate role alternation
	if err := m.addEntry(ctx, "user", "Hello"); err != nil {
		t.Errorf("failed to add user entry: %v", err)
	}

	if err := m.addEntry(ctx, "model", "Hi there"); err != nil {
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

	_ = m.addEntry(ctx, "user", "Initial")
	m.snapshot()

	_ = m.addEntry(ctx, "model", "Response")
	if len(m.GetContents()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m.GetContents()))
	}

	m.rollback(ctx)
	if len(m.GetContents()) != 1 {
		t.Errorf("expected 1 entry after rollback, got %d", len(m.GetContents()))
	}
	if m.GetContents()[0].Parts[0].Text != "Initial" {
		t.Errorf("expected 'Initial', got '%s'", m.GetContents()[0].Parts[0].Text)
	}

	// rollback with no snapshot should do nothing (or at least not crash)
	m3 := NewManager(filepath.Join(tmpDir, "m3.json"))
	m3.rollback(ctx)
}

func TestHistoryManager_Interfaces(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "interfaces.json")
	m := NewManager(historyFile)

	if m.getPath() != historyFile {
		t.Errorf("getPath() = %s, want %s", m.getPath(), historyFile)
	}

	if m.GetResolver() == nil {
		t.Error("GetResolver() should not be nil for JSONLStore")
	}

	fs := storage.DefaultFileSystem
	m.withFileSystem(fs)
	// Verify it reached the store
	if s, ok := m.store.(*jsonlStore); ok {
		if s.fs != fs {
			t.Error("WithFileSystem did not propagate to store")
		}
	} else {
		t.Error("store is not jsonlStore")
	}

	m.setStore(m.store) // Coverage for SetStore
}

type mockStore struct{}

func (s *mockStore) Load(ctx context.Context) ([]*llm.Content, error)          { return nil, nil }
func (s *mockStore) Save(ctx context.Context, contents []*llm.Content) error   { return nil }
func (s *mockStore) Append(ctx context.Context, contents []*llm.Content) error { return nil }

func TestHistoryManager_GetResolver_Nil(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "history.json"))
	m.setStore(&mockStore{})
	if m.GetResolver() != nil {
		t.Error("expected nil resolver for mockStore")
	}
}

func TestHistoryManager_FileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "new_subdir", "history.jsonL")

	h := NewManager(historyFile)
	ctx := context.Background()

	// Add an entry to trigger a save
	err := h.addEntry(ctx, "user", "test message")
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(historyFile); os.IsNotExist(err) {
		t.Errorf("expected history file to be created at %s", historyFile)
	}
}

func TestHistoryManager_PinValidTurn(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "pin_valid.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Setup: 2 turns (4 messages)
	_ = m.addEntry(ctx, "user", "U1")
	_ = m.addEntry(ctx, "model", "M1")
	_ = m.addEntry(ctx, "user", "U2")
	_ = m.addEntry(ctx, "model", "M2")

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
}

func TestHistoryManager_UnpinTurn(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "unpin.json")
	m := NewManager(historyFile)
	ctx := context.Background()

	// Setup: 1 turn pinned
	_ = m.addEntry(ctx, "user", "U1")
	_ = m.addEntry(ctx, "model", "M1")
	if err := m.SetPinned(ctx, 0, true); err != nil {
		t.Fatalf("SetPinned(0, true) failed: %v", err)
	}

	if err := m.SetPinned(ctx, 0, false); err != nil {
		t.Fatalf("SetPinned(0, false) failed: %v", err)
	}
	contents := m.GetContents()
	if contents[0].Pinned || contents[1].Pinned {
		t.Error("Turn 0 should be unpinned")
	}
}

func TestHistoryManager_ClonePersistent(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "history.json"))
	ctx := context.Background()

	content := &llm.Content{
		Role:           "user",
		Parts:          []*llm.Part{{Text: "persistent"}},
		TransientParts: []*llm.Part{{Text: "transient"}},
	}

	err := m.AddContent(ctx, content)
	if err != nil {
		t.Fatalf("AddContent failed: %v", err)
	}

	contents := m.GetContents()
	if len(contents) != 1 {
		t.Fatalf("expected 1 message, got %d", len(contents))
	}

	if len(contents[0].TransientParts) != 0 {
		t.Error("TransientParts should have been omitted in history")
	}

	if len(contents[0].Parts) != 1 || contents[0].Parts[0].Text != "persistent" {
		t.Error("persistent parts should be preserved")
	}
}
