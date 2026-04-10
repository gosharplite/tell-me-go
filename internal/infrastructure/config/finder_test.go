// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigFinder_Find(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	
	// Create a configs directory and an assistant.yaml file
	configPath := filepath.Join(tmpDir, "configs", "assistant.yaml")
	err := os.MkdirAll(filepath.Dir(configPath), 0755)
	if err != nil {
		t.Fatalf("failed to create configs directory: %v", err)
	}
	err = os.WriteFile(configPath, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("failed to create assistant.yaml: %v", err)
	}

	// Use WithBaseDir to avoid dependency on global working directory
	finder := NewDefaultConfigFinder(WithBaseDir(tmpDir))
	path, err := finder.Find()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, "configs", "assistant.yaml")
	if path != expectedPath {
		t.Errorf("got path %q; want %q", path, expectedPath)
	}
}
