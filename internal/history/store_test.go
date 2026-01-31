// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

func TestJSONLStore_LargeLine(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "large_history.jsonl")
	store := NewJSONLStore(filePath)

	// Create a very large entry (e.g., 200KB, which is > 64KB default bufio.Scanner limit)
	largeText := strings.Repeat("A", 200*1024)
	largeContent := &types.Content{
		Role:  genai.RoleUser,
		Parts: []*types.Part{{Text: largeText}},
	}

	// Test Append
	if err := store.Append(largeContent); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Test Load
	contents, err := store.Load()
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
	_, err := store.Load()
	if err == nil {
		t.Error("expected error for malformed JSONL, got nil")
	}
}
