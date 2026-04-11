// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigFinder_Find(t *testing.T) {
	t.Run("Local Directory configs/assistant.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "configs", "assistant.yaml")
		err := os.MkdirAll(filepath.Dir(configPath), 0755)
		if err != nil {
			t.Fatalf("failed to create configs directory: %v", err)
		}
		err = os.WriteFile(configPath, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("failed to create assistant.yaml: %v", err)
		}

		finder := NewDefaultConfigFinder(WithBaseDir(tmpDir))
		path, err := finder.Find()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedPath := filepath.Join(tmpDir, "configs", "assistant.yaml")
		if path != expectedPath {
			t.Errorf("got path %q; want %q", path, expectedPath)
		}
	})

	t.Run("Local Directory assistant.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "assistant.yaml")
		err := os.WriteFile(configPath, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("failed to create assistant.yaml: %v", err)
		}

		finder := NewDefaultConfigFinder(WithBaseDir(tmpDir))
		path, err := finder.Find()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if path != configPath {
			t.Errorf("got path %q; want %q", path, configPath)
		}
	})

	t.Run("Parent Traversal", func(t *testing.T) {
		tmpDir := t.TempDir()
		parentDir := filepath.Join(tmpDir, "parent")
		childDir := filepath.Join(parentDir, "child")
		err := os.MkdirAll(childDir, 0755)
		if err != nil {
			t.Fatalf("failed to create directories: %v", err)
		}

		configPath := filepath.Join(parentDir, ".tell-me-go.yaml")
		err = os.WriteFile(configPath, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("failed to create .tell-me-go.yaml: %v", err)
		}

		finder := NewDefaultConfigFinder(WithBaseDir(childDir))
		path, err := finder.Find()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if path != configPath {
			t.Errorf("got path %q; want %q", path, configPath)
		}
	})

	t.Run("Fallback", func(t *testing.T) {
		tmpDir := t.TempDir()
		finder := NewDefaultConfigFinder(WithBaseDir(tmpDir))
		path, err := finder.Find()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedPath := filepath.Join(tmpDir, "configs", "assistant.yaml")
		if path != expectedPath {
			t.Errorf("got path %q; want %q", path, expectedPath)
		}
	})
}
