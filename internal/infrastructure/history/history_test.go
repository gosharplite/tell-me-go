// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestHistoryManager_Basic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	archiveFile := filepath.Join(tmpDir, "history.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
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

	m2 := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
	if err := m2.Load(ctx); err != nil {
		t.Fatalf("failed to load history: %v", err)
	}

	if len(m2.GetContents()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m2.GetContents()))
	}
}

func TestHistoryManager_Load_NonExistent(t *testing.T) {
	t.Parallel()
	m := NewManager(infrapersistence.NewOSFileSystem(), "non-existent.json", "non-existent.archive.jsonl")
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
	archiveFile := filepath.Join(tmpDir, "corrupted.archive.jsonl")
	if err := os.WriteFile(historyFile, []byte("{invalid json}"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
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

	historyPath := filepath.Join(filePath, "history.json")
	archivePath := filepath.Join(filePath, "history.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyPath, archivePath)
	ctx := context.Background()
	if err := m.Save(ctx); err == nil {
		t.Error("expected error when directory creation fails, got nil")
	}
}

func TestHistoryManager_SnapshotRollback(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	archiveFile := filepath.Join(tmpDir, "history.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
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
	m3 := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "m3.json"), filepath.Join(tmpDir, "m3.archive.jsonl"))
	m3.rollback(ctx)
}

func TestHistoryManager_Interfaces(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "interfaces.json")
	archiveFile := filepath.Join(tmpDir, "interfaces.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)

	if m.getPath() != historyFile {
		t.Errorf("getPath() = %s, want %s", m.getPath(), historyFile)
	}

	if m.GetResolver() == nil {
		t.Error("GetResolver() should not be nil for JSONLStore")
	}

	fs := infrapersistence.NewOSFileSystem()
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

func (s *mockStore) Load(ctx context.Context) ([]*llm.Content, error)                    { return nil, nil }
func (s *mockStore) Save(ctx context.Context, contents []*llm.Content) error             { return nil }
func (s *mockStore) Append(ctx context.Context, contents []*llm.Content) error           { return nil }
func (s *mockStore) Archive(ctx context.Context, contents []*llm.Content) error          { return nil }
func (s *mockStore) AppendParts(ctx context.Context, index int, parts []*llm.Part) error { return nil }

func TestHistoryManager_GetResolver_Nil(t *testing.T) {
	tmpDir := t.TempDir()
	m := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	m.setStore(&mockStore{})
	if m.GetResolver() != nil {
		t.Error("expected nil resolver for mockStore")
	}
}

func TestHistoryManager_FileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "new_subdir", "history.jsonL")
	archiveFile := filepath.Join(tmpDir, "new_subdir", "history.archive.jsonl")

	h := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
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
	archiveFile := filepath.Join(tmpDir, "pin_valid.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
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
	archiveFile := filepath.Join(tmpDir, "unpin.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
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
	tmpDir := t.TempDir()
	m := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
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

func (s *mockStore) UpdateMetadata(ctx context.Context, index int, metadata map[string]interface{}) error {
	return nil
}
func (s *mockStore) Truncate(ctx context.Context, length int) error { return nil }
func (s *mockStore) Compact(ctx context.Context) error              { return nil }

type mockStoreErrorMetadata struct {
	mockStore
	err         error
	failOnIndex int
}

func (s *mockStoreErrorMetadata) UpdateMetadata(ctx context.Context, index int, metadata map[string]interface{}) error {
	if index == s.failOnIndex {
		return s.err
	}
	return nil
}

func TestHistoryManager_SetPinned_Error(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	ctx := context.Background()

	_ = m.addEntry(ctx, "user", "U1")
	_ = m.addEntry(ctx, "model", "M1")

	expectedErr := errors.New("update failed")

	// Test failure on first UpdateMetadata
	m.setStore(&mockStoreErrorMetadata{err: expectedErr, failOnIndex: 0})
	err := m.SetPinned(ctx, 0, true)
	if err == nil || err.Error() != expectedErr.Error() {
		t.Errorf("expected error %v on first update, got %v", expectedErr, err)
	}

	// Test failure on second UpdateMetadata
	m.setStore(&mockStoreErrorMetadata{err: expectedErr, failOnIndex: 1})
	err = m.SetPinned(ctx, 0, true)
	if err == nil || err.Error() != expectedErr.Error() {
		t.Errorf("expected error %v on second update, got %v", expectedErr, err)
	}
}

func TestHistoryManager_SetPinned_InvalidIndex(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	ctx := context.Background()

	_ = m.addEntry(ctx, "user", "U1")
	_ = m.addEntry(ctx, "model", "M1")

	// Invalid index (negative)
	if err := m.SetPinned(ctx, -1, true); err == nil {
		t.Error("expected error for negative index, got nil")
	}

	// Invalid index (out of bounds)
	if err := m.SetPinned(ctx, 1, true); err == nil {
		t.Error("expected error for out of bounds index, got nil")
	}
}

func TestHistoryManager_SetContents(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	ctx := context.Background()

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "Hi"}}},
	}

	if err := m.SetContents(ctx, contents); err != nil {
		t.Fatalf("SetContents failed: %v", err)
	}

	loaded := m.GetContents()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(loaded))
	}
	if loaded[0].Role != "user" || loaded[1].Role != "model" {
		t.Errorf("contents role mismatch")
	}
}

func TestHistoryManager_AppendParts(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	ctx := context.Background()

	_ = m.addEntry(ctx, "user", "U1")
	_ = m.addEntry(ctx, "model", "M1")

	// Append parts to existing index
	err := m.AppendParts(ctx, 1, []*llm.Part{{Text: " appended M1"}})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}

	contents := m.GetContents()
	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}

	if len(contents[1].Parts) != 2 {
		t.Fatalf("expected 2 parts in model message, got %d", len(contents[1].Parts))
	}

	if contents[1].Parts[0].Text != "M1" || contents[1].Parts[1].Text != " appended M1" {
		t.Errorf("got wrong parts: %v", contents[1].Parts)
	}

	// Test invalid index
	err = m.AppendParts(ctx, -1, []*llm.Part{{Text: "test"}})
	if err == nil {
		t.Error("expected error for negative index")
	}
	err = m.AppendParts(ctx, 5, []*llm.Part{{Text: "test"}})
	if err == nil {
		t.Error("expected error for out of bounds index")
	}
}

func TestHistoryManager_Archive(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	archiveFile := filepath.Join(tmpDir, "history.archive.jsonl")
	m := NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
	ctx := context.Background()

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Initial message"}}},
	}

	if err := m.Archive(ctx, contents); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	// Verify archive exists
	if _, err := os.Stat(archiveFile); os.IsNotExist(err) {
		t.Error("archive file was not created")
	}
}
