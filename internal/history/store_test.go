// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestJSONLStore_LargeLine(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large_history.jsonl")
	store := NewJSONLStore(filePath)
	ctx := context.Background()

	// Create a very large entry (e.g., 200KB, which is > 64KB default bufio.Scanner limit)
	largeText := strings.Repeat("A", 200*1024)
	largeContent := &types.Content{
		Role:  "user",
		Parts: []*types.Part{{Text: largeText}},
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
	content := &types.Content{
		Role: "user",
		Parts: []*types.Part{
			{InlineData: &types.Blob{MIMEType: "image/png", Data: data}},
		},
	}

	// 1. Save
	if err := store.Save(ctx, []*types.Content{content}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 2. Verify JSON file does NOT contain the raw data but has an AssetID
	rawJSON, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(rawJSON)
	if strings.Contains(jsonStr, "fake-image-data") {
		t.Error("JSON still contains raw binary data")
	}
	if !strings.Contains(jsonStr, "asset_id") {
		t.Error("JSON missing asset_id")
	}

	// 3. Verify asset exists in assets directory
	assetDir := filepath.Join(tmpDir, "assets")
	files, _ := os.ReadDir(assetDir)
	if len(files) == 0 {
		t.Error("Assets directory is empty")
	}

	// 4. Load and verify NO eager hydration
	loaded, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Parts[0].InlineData == nil {
		t.Fatal("Failed to load content or parts")
	}
	if len(loaded[0].Parts[0].InlineData.Data) != 0 {
		t.Error("Data should not be eagerly hydrated")
	}

	// 5. Verify manual hydration via Resolve
	assetID := loaded[0].Parts[0].AssetID
	if assetID == "" {
		t.Fatal("AssetID should not be empty")
	}

	resolvedData, err := store.Resolve(ctx, assetID)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if string(resolvedData) != "fake-image-data" {
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
