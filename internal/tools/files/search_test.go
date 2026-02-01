// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestSearchFiles_SkipsBinary(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "search_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	binaryPath := filepath.Join(tempDir, "binary.bin")
	err = os.WriteFile(binaryPath, []byte{0, 1, 2, 3}, 0644)
	if err != nil {
		t.Fatal(err)
	}

	textPath := filepath.Join(tempDir, "text.txt")
	err = os.WriteFile(textPath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	sm := security.NewSecurityManager(nil)
	s := &fileSearcher{sm: sm, fs: fsutil.DefaultFileSystem}

	ctx := context.Background()
	args := map[string]interface{}{
		"path":  tempDir,
		"query": "hello",
	}

	result, err := s.searchFiles(ctx, args)
	if err != nil {
		t.Fatalf("searchFiles failed: %v", err)
	}

	if !strings.Contains(result.Text, "text.txt:1: hello world") {
		t.Errorf("expected result to contain text file match, got %q", result.Text)
	}

	if strings.Contains(result.Text, "binary.bin") {
		t.Error("expected result NOT to contain binary file match")
	}
}
