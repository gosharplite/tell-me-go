// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestJSONLStore_LargeLine(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large_history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	// Create a very large entry (e.g., 200KB, which is > 64KB default bufio.Scanner limit)
	largeText := strings.Repeat("A", 200*1024)
	largeContent := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: largeText}},
	}

	// Test Append
	if err := store.Append(ctx, []*llm.Content{largeContent}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Test Load
	contents, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(contents))
	}

	if len(contents[0].Parts[0].Text) != 200*1024 {
		t.Errorf("content length mismatch: got %d, want %d", len(contents[0].Parts[0].Text), 200*1024)
	}
}

func TestJSONLStore_AssetExternalization(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	data := []byte("fake-image-data")
	content := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: data}},
		},
	}

	t.Run("Save", func(t *testing.T) {
		if err := store.Save(ctx, []*llm.Content{content}); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	})

	t.Run("VerifyJSONFile", func(t *testing.T) {
		verifyJSONFile(t, filePath, "fake-image-data")
	})

	t.Run("VerifyAssetDirectory", func(t *testing.T) {
		verifyAssetDir(t, tmpDir)
	})

	var loaded []*llm.Content
	t.Run("LoadWithoutHydration", func(t *testing.T) {
		var err error
		loaded, err = store.Load(ctx)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		verifyLoaded(t, loaded)
	})

	t.Run("ResolveAsset", func(t *testing.T) {
		verifyResolve(t, ctx, store, loaded, "fake-image-data")
	})
}

func verifyJSONFile(t *testing.T, filePath string, unexpectedData string) {
	t.Helper()
	rawJSON, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(rawJSON)
	if strings.Contains(jsonStr, unexpectedData) {
		t.Error("JSON still contains raw binary data")
	}
	if !strings.Contains(jsonStr, "asset_id") {
		t.Error("JSON missing asset_id")
	}
}

func verifyAssetDir(t *testing.T, tmpDir string) {
	t.Helper()
	assetDir := filepath.Join(tmpDir, "assets")
	files, _ := os.ReadDir(assetDir)
	if len(files) == 0 {
		t.Error("Assets directory is empty")
	}
}

func verifyLoaded(t *testing.T, loaded []*llm.Content) {
	t.Helper()
	if len(loaded) != 1 || loaded[0].Parts[0].InlineData == nil {
		t.Fatal("Failed to load content or parts")
	}
	if len(loaded[0].Parts[0].InlineData.Data) != 0 {
		t.Error("Data should not be eagerly hydrated")
	}
}

func verifyResolve(t *testing.T, ctx context.Context, store *jsonlStore, loaded []*llm.Content, expectedData string) {
	t.Helper()
	if len(loaded) == 0 {
		t.Skip("No loaded content to test Resolve")
	}
	assetID := loaded[0].Parts[0].AssetID
	if assetID == "" {
		t.Fatal("AssetID should not be empty")
	}

	resolvedData, err := store.Resolve(ctx, assetID)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if string(resolvedData) != expectedData {
		t.Errorf("Resolved data mismatch: got %s", string(resolvedData))
	}
}

func TestJSONLStore_MalformedLine(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "malformed.jsonl")

	// Write one good line and one bad line
	content := `{"Role":"user", "Parts":[{"Text":"hello"}]}` + "\n" + `{"Role":"model", "Parts":` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	_, err := store.Load(ctx)
	if err == nil {
		t.Error("expected error for malformed JSONL, got nil")
	}
}

func TestJSONLStore_WithFileSystem(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "fs_test.jsonl")
	fs := infrapersistence.NewOSFileSystem()
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl")).withFileSystem(fs)

	if store.fs != fs {
		t.Error("withFileSystem failed to set filesystem on jsonlStore")
	}
}

func TestJSONLStore_Append_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Append(ctx, []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestJSONLStore_Load_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel_load.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	_ = store.Save(ctx, []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}}})

	ctx2, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Load(ctx2)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestJSONLStore_Save_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel_save.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data := []byte("image")
	content := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: data}},
		},
	}

	err := store.Save(ctx, []*llm.Content{content})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestJSONLStore_PinnedPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "pinned_history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	content := &llm.Content{
		Role:   "user",
		Parts:  []*llm.Part{{Text: "Critical decision"}},
		Pinned: true,
	}

	// Save pinned content
	if err := store.Save(ctx, []*llm.Content{content}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load it back
	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	if !loaded[0].Pinned {
		t.Error("expected Pinned to be true after loading")
	}

	// Test Append with Pinned: true
	appendContent := &llm.Content{
		Role:   "model",
		Parts:  []*llm.Part{{Text: "Acknowledged"}},
		Pinned: true,
	}
	if err := store.Append(ctx, []*llm.Content{appendContent}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	loaded, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}

	if !loaded[1].Pinned {
		t.Error("expected second entry Pinned to be true after loading")
	}
}

func TestJSONLStore_NoTransientPartsLeaking(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "transient_test.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	content := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "Hello"}},
		TransientParts: []*llm.Part{
			{Text: "Internal thought that should not be saved"},
		},
	}

	if err := store.Save(ctx, []*llm.Content{content}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	if len(loaded[0].TransientParts) != 0 {
		t.Errorf("expected 0 TransientParts, got %d", len(loaded[0].TransientParts))
	}

	// Double check raw JSON
	rawJSON, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawJSON), "Internal thought") {
		t.Error("JSON contains TransientParts data")
	}
}

func TestJSONLStore_PrepareForStorage_EmptyInput(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty_input.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	prepared, err := store.prepareForStorage(ctx, nil)
	if err != nil {
		t.Fatalf("prepareForStorage failed: %v", err)
	}
	if prepared != nil {
		t.Error("expected nil for nil input")
	}
}

func TestJSONLStore_PrepareForStorage_PathPermissionErrors(t *testing.T) {
	tmpDir := t.TempDir()
	// Simulate by using a path that cannot be a directory for assets
	invalidDir := filepath.Join(tmpDir, "a-file")
	if err := os.WriteFile(invalidDir, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	badStore := newJSONLStore(infrapersistence.NewOSFileSystem(), filepath.Join(invalidDir, "history.jsonl"), filepath.Join(invalidDir, "history.archive.jsonl"))
	ctx := context.Background()
	content := &llm.Content{
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("data")}},
		},
	}
	_, err := badStore.prepareForStorage(ctx, content)
	if err == nil {
		t.Error("expected error when asset directory cannot be created")
	}
}

func verifyPreparedContent(t *testing.T, prepared *llm.Content) {
	t.Helper()
	if prepared.TokenCount != 123 {
		t.Errorf("expected TokenCount 123, got %d", prepared.TokenCount)
	}

	if len(prepared.Parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(prepared.Parts))
	}

	if prepared.Parts[0].Text != "Hello" {
		t.Errorf("expected first part text 'Hello', got %s", prepared.Parts[0].Text)
	}

	if prepared.Parts[1].AssetID == "" {
		t.Error("expected second part to have AssetID")
	}

	if prepared.Parts[1].InlineData.Data != nil {
		t.Error("expected second part data to be nil after storage prep")
	}

	if prepared.Parts[2].FunctionCall == nil || prepared.Parts[2].FunctionCall.Name != "get_weather" {
		t.Error("FunctionCall not preserved or incorrect")
	}

	if prepared.Parts[3].FunctionResponse == nil || prepared.Parts[3].FunctionResponse.Name != "get_weather" {
		t.Error("FunctionResponse not preserved or incorrect")
	}
}

func TestJSONLStore_PrepareForStorage_MixedContentParts(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "mixed_parts.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	content := &llm.Content{
		Role:       "user",
		TokenCount: 123,
		Parts: []*llm.Part{
			{Text: "Hello"},
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("data")}},
			{
				FunctionCall: &llm.FunctionCall{
					Name: "get_weather",
					Args: map[string]interface{}{"location": "London"},
				},
			},
			{
				FunctionResponse: &llm.FunctionResponse{
					Name:     "get_weather",
					Response: map[string]interface{}{"temp": 20},
				},
			},
		},
	}

	prepared, err := store.prepareForStorage(ctx, content)
	if err != nil {
		t.Fatalf("prepareForStorage failed: %v", err)
	}

	verifyPreparedContent(t, prepared)
}

func TestJSONLStore_UpdateMetadata_SaveAndPatch(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "patches.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "Msg 2"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 3"}}},
	}
	if err := store.Save(ctx, contents); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := store.UpdateMetadata(ctx, 0, map[string]interface{}{"pinned": true}); err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(loaded))
	}
	if !loaded[0].Pinned {
		t.Error("expected first entry to be pinned")
	}
	if loaded[1].Pinned {
		t.Error("expected second entry to not be pinned")
	}
}

func TestJSONLStore_UpdateMetadata_Compact(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "patches_compact.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 1"}}, Pinned: true},
		{Role: "model", Parts: []*llm.Part{{Text: "Msg 2"}}},
	}
	if err := store.Save(ctx, contents); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := store.Compact(ctx); err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries after compact, got %d", len(loaded))
	}
	if !loaded[0].Pinned {
		t.Error("expected first entry to still be pinned after compact")
	}

	rawJSON, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawJSON), "_patch") {
		t.Error("compacted file should not contain patches")
	}
}

func TestJSONLStore_Load_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.jsonl")

	// Create an empty file
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	contents, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(contents) != 0 {
		t.Errorf("expected 0 contents, got %d", len(contents))
	}
}

func TestJSONLStore_Load_LegacyJSONArray(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "legacy.json")

	// Valid JSON Array
	content := `[{"Role":"user", "Parts":[{"Text":"hello legacy"}]}]`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	contents, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("expected no error for legacy JSON array, got %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0].Parts[0].Text != "hello legacy" {
		t.Errorf("expected 'hello legacy', got '%s'", contents[0].Parts[0].Text)
	}
}

func TestJSONLStore_Load_InvalidLegacyJSONArray(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid_legacy.json")

	// Invalid JSON Array (starts with '[' but is malformed)
	content := `[{"Role":"user", `
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	_, err := store.Load(ctx)
	// Fallback to JSONL decoder which will fail on '[' token
	if err == nil {
		t.Error("expected error for invalid legacy JSON array, got nil")
	}
}

func TestJSONLStore_MarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "marshal_error.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	// Inject a channel into Parts to force a json.Marshal error
	ch := make(chan int)
	content := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{FunctionResponse: &llm.FunctionResponse{Response: map[string]interface{}{"ch": ch}}},
		},
	}

	err := store.Append(ctx, []*llm.Content{content})
	if err == nil {
		t.Error("expected error during Append due to marshal failure")
	}

	err = store.Save(ctx, []*llm.Content{content})
	if err == nil {
		t.Error("expected error during Save due to marshal failure")
	}
}

func TestJSONLStore_MkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file where a directory needs to be
	filePath := filepath.Join(tmpDir, "file")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filepath.Join(filePath, "history.jsonl"), filepath.Join(filePath, "history.archive.jsonl"))
	ctx := context.Background()

	// Append requires directory
	err := store.Append(ctx, []*llm.Content{{Role: "user"}})
	if err == nil {
		t.Error("expected error during Append due to MkdirAll failure")
	}

	// UpdateMetadata requires directory
	err = store.UpdateMetadata(ctx, 0, map[string]interface{}{"pinned": true})
	if err == nil {
		t.Error("expected error during UpdateMetadata due to MkdirAll failure")
	}
}

func TestJSONLStore_Load_ParseContentError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "parse_error.jsonl")

	// Valid JSON but invalid for llm.Content (role should be string)
	content := `{"Role": 123}` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	_, err := store.Load(ctx)
	if err == nil {
		t.Error("expected error for invalid type in JSON, got nil")
	}
}

func TestJSONLStore_CompactError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "compact_error.jsonl")

	// Write a file that will fail to load
	content := `{"Role": 123}` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	err := store.Compact(ctx)
	if err == nil {
		t.Error("expected error during Compact due to Load failure")
	}
}

func TestJSONLStore_Load_ReadFileError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory where the file should be
	filePath := filepath.Join(tmpDir, "dir_not_file.jsonl")
	if err := os.Mkdir(filePath, 0755); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()
	_, err := store.Load(ctx)
	if err == nil {
		t.Error("expected error when trying to read a directory, got nil")
	}
}

type mockFS struct {
	persistence.FileSystem
	mkdirErrFunc func(string) error
	writeErr     error
	readErr      error
	openErr      error
	closeErr     error
	removeErr    error
	statErr      error
	readAtErr    error
}

func (m *mockFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if m.statErr != nil {
		return nil, m.statErr
	}
	if m.mkdirErrFunc != nil {
		if err := m.mkdirErrFunc(name); err != nil {
			return nil, os.ErrNotExist
		}
	}
	return m.FileSystem.Stat(context.Background(), name)
}

func (m *mockFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if m.mkdirErrFunc != nil {
		if err := m.mkdirErrFunc(path); err != nil {
			return err
		}
	}
	return m.FileSystem.MkdirAll(context.Background(), path, perm)
}

func (m *mockFS) Open(ctx context.Context, name string) (persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	f, err := m.FileSystem.Open(context.Background(), name)
	if err != nil {
		return nil, err
	}
	return &mockFileWithErr{File: f, writeErr: m.writeErr, closeErr: m.closeErr, readAtErr: m.readAtErr}, nil
}

func (m *mockFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	f, err := m.FileSystem.OpenFile(context.Background(), name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &mockFileWithErr{File: f, writeErr: m.writeErr, closeErr: m.closeErr, readAtErr: m.readAtErr}, nil
}

func (m *mockFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	return m.FileSystem.WriteFile(context.Background(), name, data, perm)
}

func (m *mockFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	return m.FileSystem.AtomicWrite(context.Background(), name, data, perm)
}

func (m *mockFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	return m.FileSystem.ReadFile(context.Background(), name)
}

func (m *mockFS) Remove(ctx context.Context, name string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	return m.FileSystem.Remove(context.Background(), name)
}

type mockFileWithErr struct {
	persistence.File
	writeErr  error
	closeErr  error
	readAtErr error
}

func (f *mockFileWithErr) Write(p []byte) (n int, err error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(p)
}

func (f *mockFileWithErr) Close() error {
	if f.closeErr != nil {
		_ = f.File.Close() // best effort
		return f.closeErr
	}
	return f.File.Close()
}

func (f *mockFileWithErr) ReadAt(p []byte, off int64) (n int, err error) {
	if f.readAtErr != nil {
		return 0, f.readAtErr
	}
	return f.File.ReadAt(p, off)
}

func TestJSONLStore_IOErrors(t *testing.T) {

	dummyContent := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}}}

	tests := []struct {
		name      string
		setupMock func(*mockFS)
		action    func(context.Context, *jsonlStore) error
		wantErr   string
	}{
		{
			name: "MkdirAll Failure on Append",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error { return errors.New("mkdir failed") }
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Append(ctx, dummyContent)
			},
			wantErr: "mkdir failed",
		},
		{
			name: "MkdirAll Failure on Save",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error { return errors.New("mkdir failed") }
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Save(ctx, dummyContent)
			},
			wantErr: "mkdir failed",
		},
		{
			name: "MkdirAll Failure on UpdateMetadata",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error { return errors.New("mkdir failed") }
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.UpdateMetadata(ctx, 0, map[string]interface{}{})
			},
			wantErr: "mkdir failed",
		},
		{
			name: "MkdirAll Failure on Archive",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error { return errors.New("mkdir failed") }
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Archive(ctx, dummyContent)
			},
			wantErr: "mkdir failed",
		},
		{
			name: "MkdirAll Failure on AppendParts",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error { return errors.New("mkdir failed") }
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.AppendParts(ctx, 0, []*llm.Part{{Text: "test"}})
			},
			wantErr: "mkdir failed",
		},
		{
			name:      "OpenFile Failure on Append",
			setupMock: func(m *mockFS) { m.openErr = errors.New("open failed") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Append(ctx, dummyContent)
			},
			wantErr: "open failed",
		},
		{
			name:      "Write Failure on Append",
			setupMock: func(m *mockFS) { m.writeErr = errors.New("write failed") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Append(ctx, dummyContent)
			},
			wantErr: "write failed",
		},
		{
			name:      "AtomicWrite Failure on Save",
			setupMock: func(m *mockFS) { m.writeErr = errors.New("atomic write failed") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Save(ctx, dummyContent)
			},
			wantErr: "atomic write failed",
		},
		{
			name:      "Write Failure on UpdateMetadata",
			setupMock: func(m *mockFS) { m.writeErr = errors.New("write failed") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.UpdateMetadata(ctx, 0, map[string]interface{}{"pinned": true})
			},
			wantErr: "write failed",
		},
		{
			name:      "Context Canceled on Append",
			setupMock: func(m *mockFS) {},
			action: func(ctx context.Context, s *jsonlStore) error {
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				return s.Append(ctxCanceled, dummyContent)
			},
			wantErr: "context canceled",
		},
		{
			name:      "Context Canceled on UpdateMetadata",
			setupMock: func(m *mockFS) {},
			action: func(ctx context.Context, s *jsonlStore) error {
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				return s.UpdateMetadata(ctxCanceled, 0, map[string]interface{}{"pinned": true})
			},
			wantErr: "context canceled",
		},
		{
			name:      "Context Canceled on Load",
			setupMock: func(m *mockFS) {},
			action: func(ctx context.Context, s *jsonlStore) error {
				_ = s.Append(context.Background(), dummyContent)
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				_, err := s.Load(ctxCanceled)
				return err
			},
			wantErr: "context canceled",
		},
		{
			name: "AssetStore Put Failure on Append",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error {
					if strings.Contains(path, "assets") {
						return errors.New("asset mkdir failed")
					}
					return nil
				}
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				// ensure history dir exists so ensureDirectory doesn't fail
				dir := filepath.Dir(s.filePath)
				_ = infrapersistence.NewOSFileSystem().MkdirAll(context.Background(), dir, 0755)

				contentWithAsset := []*llm.Content{
					{Role: "user", Parts: []*llm.Part{
						{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("img")}},
					}},
				}
				return s.Append(ctx, contentWithAsset)
			},
			wantErr: "asset mkdir failed",
		},
		{
			name:      "Close Failure on Append",
			setupMock: func(m *mockFS) { m.closeErr = errors.New("simulated close error") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Append(ctx, dummyContent)
			},
			wantErr: "", // Close error after successful Sync is logged, not returned
		},
		{
			name:      "Close Failure on Archive",
			setupMock: func(m *mockFS) { m.closeErr = errors.New("simulated close error") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Archive(ctx, dummyContent)
			},
			wantErr: "", // Close error after successful Sync is logged, not returned
		},
		{
			name:      "Close Failure on UpdateMetadata",
			setupMock: func(m *mockFS) { m.closeErr = errors.New("simulated close error") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.UpdateMetadata(ctx, 0, map[string]interface{}{"pinned": true})
			},
			wantErr: "", // Close error after successful Sync is logged, not returned
		},
		{
			name:      "Close Failure on AppendParts",
			setupMock: func(m *mockFS) { m.closeErr = errors.New("simulated close error") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.AppendParts(ctx, 0, []*llm.Part{{Text: "test"}})
			},
			wantErr: "", // Close error after successful Sync is logged, not returned
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.jsonl")

			baseFS := infrapersistence.NewOSFileSystem()
			mfs := &mockFS{FileSystem: baseFS}
			tt.setupMock(mfs)

			store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl")).withFileSystem(mfs)

			err := tt.action(context.Background(), store)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestJSONLStore_UpdateMetadata_MarshalError(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))

	// Pass something unmarshalable (like a function or channel)
	unmarshalable := map[string]interface{}{"bad": make(chan int)}

	err := store.UpdateMetadata(context.Background(), 0, unmarshalable)
	if err == nil {
		t.Error("expected error for unmarshalable metadata")
	}
}

func TestJSONLStore_Append_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty_append.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))

	err := store.Append(context.Background(), []*llm.Content{})
	if err != nil {
		t.Errorf("expected no error for empty append, got %v", err)
	}
}

func TestJSONLStore_AppendParts(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "append_parts_history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	// 1. Initial history
	content := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "initial part"}},
	}
	err := store.Append(ctx, []*llm.Content{content})
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// 2. Append parts
	err = store.AppendParts(ctx, 0, []*llm.Part{{Text: " appended part"}})
	if err != nil {
		t.Fatalf("AppendParts failed: %v", err)
	}

	// 3. Load and verify
	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 content, got %d", len(loaded))
	}
	if len(loaded[0].Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(loaded[0].Parts))
	}
	if loaded[0].Parts[0].Text != "initial part" {
		t.Errorf("expected 'initial part', got %q", loaded[0].Parts[0].Text)
	}
	if loaded[0].Parts[1].Text != " appended part" {
		t.Errorf("expected ' appended part', got %q", loaded[0].Parts[1].Text)
	}
}

func TestJSONLStore_AppendParts_ErrorDir(t *testing.T) {
	// Create an invalid store path
	invalidDir := "/invalid_dir"
	if runtime.GOOS == "windows" {
		invalidDir = `C:\NUL\invalid`
	}
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filepath.Join(invalidDir, "file.jsonl"), filepath.Join(invalidDir, "archive.jsonl"))
	err := store.AppendParts(context.Background(), 0, []*llm.Part{{Text: "test"}})
	if err == nil {
		t.Error("expected error appending parts to invalid directory")
	}
}

func TestJSONLStore_Archive(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "Msg 2"}}},
	}

	// 1. Test Archive empty contents
	if err := store.Archive(ctx, []*llm.Content{}); err != nil {
		t.Errorf("expected no error for empty archive, got %v", err)
	}

	// 2. Test Archive contents
	if err := store.Archive(ctx, contents); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	// 3. Verify archive file exists and contains contents
	archivePath := filepath.Join(tmpDir, "archive.jsonl")
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Fatal("archive file does not exist")
	}

	// Use a temporary store to load the archive file
	archiveStore := newJSONLStore(infrapersistence.NewOSFileSystem(), archivePath, filepath.Join(filepath.Dir(archivePath), "archive.jsonl"))
	archivedContents, err := archiveStore.Load(ctx)
	if err != nil {
		t.Fatalf("failed to load archive: %v", err)
	}

	if len(archivedContents) != 2 {
		t.Fatalf("expected 2 archived entries, got %d", len(archivedContents))
	}

	if archivedContents[0].Parts[0].Text != "Msg 1" {
		t.Errorf("expected 'Msg 1', got %q", archivedContents[0].Parts[0].Text)
	}

	// 4. Test Archive more contents (append mode)
	moreContents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 3"}}},
	}
	if err := store.Archive(ctx, moreContents); err != nil {
		t.Fatalf("Archive failed: %v", err)
	}

	archivedContents, err = archiveStore.Load(ctx)
	if err != nil {
		t.Fatalf("failed to load archive again: %v", err)
	}

	if len(archivedContents) != 3 {
		t.Fatalf("expected 3 archived entries, got %d", len(archivedContents))
	}
}

func TestJSONLStore_Migration(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "history.json")
	jsonlPath := filepath.Join(tmpDir, "history.jsonl")

	// 1. Create legacy history.json
	content := `[{"Role":"user", "Parts":[{"Text":"migrated"}]}]`
	if err := os.WriteFile(jsonPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Initialize store with .jsonl path
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), jsonlPath, filepath.Join(filepath.Dir(jsonlPath), "archive.jsonl"))
	ctx := context.Background()

	// 3. Load should trigger migration
	contents, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(contents))
	}
	if contents[0].Parts[0].Text != "migrated" {
		t.Errorf("expected 'migrated', got %q", contents[0].Parts[0].Text)
	}

	// 4. Verify history.jsonl exists
	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		t.Error("history.jsonl was not created during migration")
	}

	// 5. Verify history.json is removed
	if _, err := os.Stat(jsonPath); err == nil {
		t.Error("legacy history.json was not removed after migration")
	}
}

func TestJSONLStore_NilPartsSanitization(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nil_parts.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))
	ctx := context.Background()

	// 1. Manually write a JSONL line with a null part
	content := `{"role":"user", "parts":[{"text":"first"}, null, {"text":"third"}]}` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Load and verify sanitization
	contents, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(contents) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(contents))
	}

	if len(contents[0].Parts) != 2 {
		t.Fatalf("expected 2 parts after sanitization, got %d", len(contents[0].Parts))
	}

	if contents[0].Parts[0].Text != "first" || contents[0].Parts[1].Text != "third" {
		t.Errorf("parts content mismatch: got %v and %v", contents[0].Parts[0].Text, contents[0].Parts[1].Text)
	}
}

type storeErrorTestCase struct {
	name          string
	setup         func(fs *mockFS, filePath string)
	action        func(ctx context.Context, s *jsonlStore) error
	expectedErr   error
	errorContains string
}

func setupErrorPathTest(t *testing.T, tt storeErrorTestCase) (*jsonlStore, *mockFS, string) {
	t.Helper()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "history.jsonl")

	baseFS := infrapersistence.NewOSFileSystem()
	mfs := &mockFS{FileSystem: baseFS}
	if tt.setup != nil {
		tt.setup(mfs, filePath)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl")).withFileSystem(mfs)
	return store, mfs, filePath
}

func assertStoreErrorPath(t *testing.T, err error, tt storeErrorTestCase) {
	t.Helper()
	if tt.expectedErr != nil {
		if !errors.Is(err, tt.expectedErr) {
			t.Errorf("%s: expected error %v, got %v", tt.name, tt.expectedErr, err)
		}
		return
	}
	if tt.errorContains != "" {
		if err == nil || !strings.Contains(err.Error(), tt.errorContains) {
			t.Errorf("%s: expected error containing %q, got %v", tt.name, tt.errorContains, err)
		}
		return
	}
	if err != nil {
		t.Errorf("%s: expected no error, got %v", tt.name, err)
	}
}

func TestStore_ErrorPaths(t *testing.T) {
	dummyContent := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}}}

	tests := []storeErrorTestCase{
		{
			name: "Load history not found (ErrHistoryNotFound)",
			setup: func(fs *mockFS, filePath string) {
				// File does not exist, so fs.Stat returns os.ErrNotExist
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				_, err := s.Load(ctx)
				return err
			},
			expectedErr: ports.ErrHistoryNotFound,
		},
		{
			name: "Load legacy JSON unmarshal failure",
			setup: func(fs *mockFS, filePath string) {
				// .json exists, .jsonl does not.
				// s.Load falls back to legacy load if migration fails or .jsonl is not found.
				// We force migration to fail by having WriteFile return an error.
				oldPath := filePath[:len(filePath)-1] // .json
				_ = fs.FileSystem.WriteFile(context.Background(), oldPath, []byte("invalid json"), 0644)
				fs.writeErr = errors.New("migration failed")
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				_, err := s.Load(ctx)
				return err
			},
			errorContains: "failed to decode legacy JSON",
		},
		{
			name: "OpenFile failure on Archive",
			setup: func(fs *mockFS, filePath string) {
				fs.openErr = errors.New("archive open failed")
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Archive(ctx, dummyContent)
			},
			errorContains: "archive open failed",
		},
		{
			name: "OpenFile failure on AppendParts",
			setup: func(fs *mockFS, filePath string) {
				fs.openErr = errors.New("appendparts open failed")
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.AppendParts(ctx, 0, []*llm.Part{{Text: "test"}})
			},
			errorContains: "appendparts open failed",
		},
		{
			name: "Marshal failure in appendSingleContent",
			setup: func(fs *mockFS, filePath string) {
				// Handled by TestJSONLStore_MarshalError, but let's add it here for completeness
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				ch := make(chan int)
				content := &llm.Content{
					Parts: []*llm.Part{
						{FunctionResponse: &llm.FunctionResponse{Response: map[string]interface{}{"ch": ch}}},
					},
				}
				return s.Append(ctx, []*llm.Content{content})
			},
			errorContains: "json: unsupported type",
		},
		{
			name: "prepareForStorage failure in AppendParts",
			setup: func(fs *mockFS, filePath string) {
				// s.prepareForStorage calls s.assetStore.Put, which calls fs.MkdirAll on "assets" dir
				fs.mkdirErrFunc = func(path string) error {
					if strings.Contains(path, "assets") {
						return errors.New("asset mkdir failed")
					}
					return nil
				}
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				_ = s.fs.MkdirAll(ctx, filepath.Dir(s.filePath), 0755)
				parts := []*llm.Part{
					{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("img")}},
				}
				return s.AppendParts(ctx, 0, parts)
			},
			errorContains: "asset mkdir failed",
		},
		{
			name: "Context cancellation in loadJSONL",
			setup: func(fs *mockFS, filePath string) {
				// Write multiple lines to ensure the loop runs
				content := `{"Role":"user"}` + "\n" + `{"Role":"model"}` + "\n"
				_ = fs.FileSystem.WriteFile(context.Background(), filePath, []byte(content), 0644)
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				_, err := s.Load(ctxCanceled)
				return err
			},
			expectedErr: context.Canceled,
		},
		{
			name: "migrateLegacyJSONFile ReadFile error",
			setup: func(fs *mockFS, filePath string) {
				oldPath := filePath[:len(filePath)-1] // .json
				_ = fs.FileSystem.WriteFile(context.Background(), oldPath, []byte("legacy"), 0644)
				fs.readErr = errors.New("read legacy failed")
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				// migrateLegacyJSONFile is called inside Load
				_, err := s.Load(ctx)
				return err
			},
			expectedErr: ports.ErrHistoryNotFound,
		},
		{
			name: "migrateLegacyJSONFile WriteFile error",
			setup: func(fs *mockFS, filePath string) {
				oldPath := filePath[:len(filePath)-1] // .json
				_ = fs.FileSystem.WriteFile(context.Background(), oldPath, []byte("legacy"), 0644)
				fs.writeErr = errors.New("write jsonl failed")
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				_, err := s.Load(ctx)
				return err
			},
			errorContains: "failed to decode legacy JSON",
		},
		{
			name: "Load Stat succeeds but ReadFile returns NotExist",
			setup: func(fs *mockFS, filePath string) {
				_ = fs.FileSystem.WriteFile(context.Background(), filePath, []byte("data"), 0644)
				fs.readErr = os.ErrNotExist
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				_, err := s.Load(ctx)
				return err
			},
			expectedErr: ports.ErrHistoryNotFound,
		},
		{
			name: "Load ReadFile returns other error",
			setup: func(fs *mockFS, filePath string) {
				_ = fs.FileSystem.WriteFile(context.Background(), filePath, []byte("data"), 0644)
				fs.readErr = errors.New("other read error")
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				_, err := s.Load(ctx)
				return err
			},
			errorContains: "other read error",
		},
		{
			name: "Marshal failure in AppendParts",
			setup: func(fs *mockFS, filePath string) {
				_ = fs.FileSystem.MkdirAll(context.Background(), filepath.Dir(filePath), 0755)
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				ch := make(chan int)
				parts := []*llm.Part{
					{FunctionResponse: &llm.FunctionResponse{Response: map[string]interface{}{"ch": ch}}},
				}
				return s.AppendParts(ctx, 0, parts)
			},
			errorContains: "json: unsupported type",
		},
		{
			name: "Context cancellation in AppendParts",
			setup: func(fs *mockFS, filePath string) {
				_ = fs.FileSystem.MkdirAll(context.Background(), filepath.Dir(filePath), 0755)
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				return s.AppendParts(ctxCanceled, 0, []*llm.Part{{Text: "test"}})
			},
			expectedErr: context.Canceled,
		},
		{
			name: "Context cancellation in UpdateMetadata",
			setup: func(fs *mockFS, filePath string) {
				_ = fs.FileSystem.MkdirAll(context.Background(), filepath.Dir(filePath), 0755)
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				return s.UpdateMetadata(ctxCanceled, 0, map[string]interface{}{})
			},
			expectedErr: context.Canceled,
		},
		{
			name: "prepareForStorage failure in Archive",
			setup: func(fs *mockFS, filePath string) {
				_ = fs.FileSystem.MkdirAll(context.Background(), filepath.Dir(filePath), 0755)
				fs.mkdirErrFunc = func(path string) error {
					if strings.Contains(path, "assets") {
						return errors.New("asset mkdir failed")
					}
					return nil
				}
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				content := []*llm.Content{
					{Parts: []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("img")}}}},
				}
				return s.Archive(ctx, content)
			},
			errorContains: "asset mkdir failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			store, _, _ := setupErrorPathTest(t, tt)
			err := tt.action(context.Background(), store)
			assertStoreErrorPath(t, err, tt)
		})
	}
}

// mockSyncCloseFile implements persistence.File with configurable Sync/Close errors.
type mockSyncCloseFile struct {
	syncErr  error
	closeErr error
	synced   bool
	closed   bool
}

func (f *mockSyncCloseFile) Read(p []byte) (int, error)                   { return 0, io.EOF }
func (f *mockSyncCloseFile) Write(p []byte) (int, error)                  { return len(p), nil }
func (f *mockSyncCloseFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *mockSyncCloseFile) ReadAt(p []byte, off int64) (int, error)      { return 0, io.EOF }
func (f *mockSyncCloseFile) ReadDir(n int) ([]os.DirEntry, error)         { return nil, nil }
func (f *mockSyncCloseFile) Sync() error                                  { f.synced = true; return f.syncErr }
func (f *mockSyncCloseFile) Close() error                                 { f.closed = true; return f.closeErr }

// mockFSForSyncClose returns a mock file from OpenFile for sync/close testing.
type mockFSForSyncClose struct {
	persistence.FileSystem
	file *mockSyncCloseFile
}

func (m *mockFSForSyncClose) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	return m.file, nil
}

func TestWithAppendFile_SyncCloseErrorPriority(t *testing.T) {
	fnErr := errors.New("fn error")
	syncErr := errors.New("sync error")
	closeErr := errors.New("close error")

	tests := []struct {
		name       string
		fnErr      error
		syncErr    error
		closeErr   error
		wantErr    error
		wantSynced bool
		wantClosed bool
	}{
		{"fn succeeds, sync succeeds, close succeeds", nil, nil, nil, nil, true, true},
		{"fn succeeds, sync fails, close succeeds", nil, syncErr, nil, syncErr, true, true},
		{"fn succeeds, sync succeeds, close fails", nil, nil, closeErr, nil, true, true},
		{"fn succeeds, sync fails, close fails", nil, syncErr, closeErr, syncErr, true, true},
		{"fn fails, sync succeeds, close succeeds", fnErr, nil, nil, fnErr, true, true},
		{"fn fails, sync fails, close succeeds", fnErr, syncErr, nil, fnErr, true, true},
		{"fn fails, sync succeeds, close fails", fnErr, nil, closeErr, fnErr, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.jsonl")

			mf := &mockSyncCloseFile{syncErr: tt.syncErr, closeErr: tt.closeErr}
			mfs := &mockFSForSyncClose{FileSystem: infrapersistence.NewOSFileSystem(), file: mf}

			store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl")).withFileSystem(mfs)

			err := store.withAppendFile(context.Background(), filePath, func(f persistence.File) error {
				return tt.fnErr
			})

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected nil error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr.Error(), err.Error())
				}
			}

			if mf.synced != tt.wantSynced {
				t.Errorf("synced = %v, want %v", mf.synced, tt.wantSynced)
			}
			if mf.closed != tt.wantClosed {
				t.Errorf("closed = %v, want %v", mf.closed, tt.wantClosed)
			}
		})
	}
}

func TestSync_CloseErrorLogged(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sync_test.jsonl")

	// Create the file so Sync doesn't get IsNotExist
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	closeErr := errors.New("close failed after sync")
	mf := &mockSyncCloseFile{closeErr: closeErr}
	mfs := &mockFSForSyncClose{FileSystem: infrapersistence.NewOSFileSystem(), file: mf}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl")).withFileSystem(mfs)

	// Capture log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	err := store.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync returned unexpected error: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "close failed after sync") {
		t.Errorf("expected log to contain 'close failed after sync', got: %s", logOutput)
	}
	if !mf.closed {
		t.Error("Close was not called")
	}
}

func TestSync_IsNotExist_ReturnsNil(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath, filepath.Join(filepath.Dir(filePath), "archive.jsonl"))

	err := store.Sync(context.Background())
	if err != nil {
		t.Errorf("Sync on non-existent file should return nil, got %v", err)
	}
}
