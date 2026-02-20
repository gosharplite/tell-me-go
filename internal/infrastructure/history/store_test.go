// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestJSONLStore_LargeLine(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large_history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "malformed.jsonl")

	// Write one good line and one bad line
	content := `{"Role":"user", "Parts":[{"Text":"hello"}]}` + "\n" + `{"Role":"model", "Parts":` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath).withFileSystem(fs)

	if store.fs != fs {
		t.Error("withFileSystem failed to set filesystem on jsonlStore")
	}
}

func TestJSONLStore_Append_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "pinned_history.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "transient_test.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty_input.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
	ctx := context.Background()

	prepared, err := store.prepareForStorage(ctx, nil)
	if err != nil {
		t.Fatalf("prepareForStorage failed: %v", err)
	}
	if prepared != nil {
		t.Error("expected nil for nil input")
	}
}

func TestJSONLStore_PrepareForStorage_MalformedJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "malformed_json.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
	ctx := context.Background()

	// prepareForStorage itself doesn't parse JSON, but we test preservation of nil parts
	content := &llm.Content{
		Parts: []*llm.Part{nil},
	}
	prepared, err := store.prepareForStorage(ctx, content)
	if err != nil {
		t.Fatalf("prepareForStorage failed: %v", err)
	}
	if len(prepared.Parts) != 1 || prepared.Parts[0] != nil {
		t.Error("expected preservation of nil part")
	}
}

func TestJSONLStore_PrepareForStorage_PathPermissionErrors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	// Simulate by using a path that cannot be a directory for assets
	invalidDir := filepath.Join(tmpDir, "a-file")
	if err := os.WriteFile(invalidDir, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	badStore := newJSONLStore(infrapersistence.NewOSFileSystem(), filepath.Join(invalidDir, "history.jsonl"))
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "mixed_parts.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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

func TestJSONLStore_UpdateMetadataAndTruncate(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "patches.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
	ctx := context.Background()

	// Initial contents
	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "Msg 2"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "Msg 3"}}},
	}

	if err := store.Save(ctx, contents); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Patch: Update metadata (pin first message)
	if err := store.UpdateMetadata(ctx, 0, map[string]interface{}{"pinned": true}); err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}

	// Load and verify
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

	// Patch: Truncate to length 2
	if err := store.Truncate(ctx, 2); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	// Load and verify
	loaded, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded))
	}
	if !loaded[0].Pinned {
		t.Error("expected first entry to still be pinned")
	}

	// Compact
	if err := store.Compact(ctx); err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Load and verify after compact
	loaded, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries after compact, got %d", len(loaded))
	}
	if !loaded[0].Pinned {
		t.Error("expected first entry to still be pinned after compact")
	}

	// Check file size / content
	rawJSON, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawJSON), "_patch") {
		t.Error("compacted file should not contain patches")
	}
}

func TestJSONLStore_Load_EmptyFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.jsonl")

	// Create an empty file
	if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "legacy.json")

	// Valid JSON Array
	content := `[{"Role":"user", "Parts":[{"Text":"hello legacy"}]}]`
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid_legacy.json")

	// Invalid JSON Array (starts with '[' but is malformed)
	content := `[{"Role":"user", `
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
	ctx := context.Background()
	_, err := store.Load(ctx)
	// Fallback to JSONL decoder which will fail on '[' token
	if err == nil {
		t.Error("expected error for invalid legacy JSON array, got nil")
	}
}

func TestJSONLStore_MarshalError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "marshal_error.jsonl")
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a file where a directory needs to be
	filePath := filepath.Join(tmpDir, "file")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filepath.Join(filePath, "history.jsonl"))
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

	// Truncate requires directory
	err = store.Truncate(ctx, 1)
	if err == nil {
		t.Error("expected error during Truncate due to MkdirAll failure")
	}
}

func TestJSONLStore_Load_ParseContentError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "parse_error.jsonl")

	// Valid JSON but invalid for llm.Content (role should be string)
	content := `{"Role": 123}` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
	ctx := context.Background()
	_, err := store.Load(ctx)
	if err == nil {
		t.Error("expected error for invalid type in JSON, got nil")
	}
}

func TestJSONLStore_CompactError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "compact_error.jsonl")

	// Write a file that will fail to load
	content := `{"Role": 123}` + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
	ctx := context.Background()
	err := store.Compact(ctx)
	if err == nil {
		t.Error("expected error during Compact due to Load failure")
	}
}

func TestJSONLStore_Load_ReadFileError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a directory where the file should be
	filePath := filepath.Join(tmpDir, "dir_not_file.jsonl")
	if err := os.Mkdir(filePath, 0755); err != nil {
		t.Fatal(err)
	}

	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)
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
	openErr      error
}

func (m *mockFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
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

func (m *mockFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	f, err := m.FileSystem.OpenFile(context.Background(), name, flag, perm)
	if err != nil {
		return nil, err
	}
	if m.writeErr != nil {
		return &mockFileWithErr{File: f, writeErr: m.writeErr}, nil
	}
	return f, nil
}

func (m *mockFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	if m.writeErr != nil {
		return errors.New("write file failed")
	}
	return m.FileSystem.WriteFile(context.Background(), name, data, perm)
}

func (m *mockFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return m.FileSystem.ReadFile(context.Background(), name)
}

type mockFileWithErr struct {
	persistence.File
	writeErr error
}

func (f *mockFileWithErr) Write(p []byte) (n int, err error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.File.Write(p)
}

func TestJSONLStore_IOErrors(t *testing.T) {
	t.Parallel()

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
			name: "MkdirAll Failure on Truncate",
			setupMock: func(m *mockFS) {
				m.mkdirErrFunc = func(path string) error { return errors.New("mkdir failed") }
			},
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Truncate(ctx, 1)
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
			name:      "WriteFile Failure on Save",
			setupMock: func(m *mockFS) { m.writeErr = errors.New("write file failed") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Save(ctx, dummyContent)
			},
			wantErr: "write file failed",
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
			name:      "Write Failure on Truncate",
			setupMock: func(m *mockFS) { m.writeErr = errors.New("write failed") },
			action: func(ctx context.Context, s *jsonlStore) error {
				return s.Truncate(ctx, 1)
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
			name:      "Context Canceled on Truncate",
			setupMock: func(m *mockFS) {},
			action: func(ctx context.Context, s *jsonlStore) error {
				ctxCanceled, cancel := context.WithCancel(ctx)
				cancel()
				return s.Truncate(ctxCanceled, 1)
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
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.jsonl")

			baseFS := infrapersistence.NewOSFileSystem()
			mfs := &mockFS{FileSystem: baseFS}
			tt.setupMock(mfs)

			store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath).withFileSystem(mfs)

			err := tt.action(context.Background(), store)
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
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)

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
	store := newJSONLStore(infrapersistence.NewOSFileSystem(), filePath)

	err := store.Append(context.Background(), []*llm.Content{})
	if err != nil {
		t.Errorf("expected no error for empty append, got %v", err)
	}
}
