// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestJSONLStore_LargeLine(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large_history.jsonl")
	store := NewJSONLStore(filePath)
	ctx := context.Background()

	// Create a very large entry (e.g., 200KB, which is > 64KB default bufio.Scanner limit)
	largeText := strings.Repeat("A", 200*1024)
	largeContent := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: largeText}},
	}

	// Test Append
	if err := store.Append(ctx, largeContent); err != nil {
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
	store := NewJSONLStore(filePath)
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

func verifyResolve(t *testing.T, ctx context.Context, store *JSONLStore, loaded []*llm.Content, expectedData string) {
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

	store := NewJSONLStore(filePath)
	ctx := context.Background()
	_, err := store.Load(ctx)
	if err == nil {
		t.Error("expected error for malformed JSONL, got nil")
	}
}

func TestJSONLStore_WithFileSystem(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "fs_test.jsonl")
	fs := &storage.OSFileSystem{}
	store := NewJSONLStore(filePath).WithFileSystem(fs)

	if store.fs != fs {
		t.Error("WithFileSystem failed to set filesystem on JSONLStore")
	}
}

func TestJSONLStore_Append_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel.jsonl")
	store := NewJSONLStore(filePath)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Append(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hi"}}})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestJSONLStore_Load_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cancel_load.jsonl")
	store := NewJSONLStore(filePath)
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
	store := NewJSONLStore(filePath)
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
	store := NewJSONLStore(filePath)
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
	if err := store.Append(ctx, appendContent); err != nil {
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
	store := NewJSONLStore(filePath)
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
	store := NewJSONLStore(filePath)
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
	store := NewJSONLStore(filePath)
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

	badStore := NewJSONLStore(filepath.Join(invalidDir, "history.jsonl"))
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

func TestJSONLStore_PrepareForStorage_MixedContentParts(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "mixed_parts.jsonl")
	store := NewJSONLStore(filePath)
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
