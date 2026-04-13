// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
)

type fileSetup struct {
	path    string
	content string
}

func setupTestFS(t *testing.T, tmpDir string, files []fileSetup) {
	t.Helper()
	for _, f := range files {
		fullPath := filepath.Join(tmpDir, f.path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(f.content), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}
}

func TestDefaultConfigFinder_Find(t *testing.T) {
	tests := []struct {
		name         string
		files        []fileSetup
		baseDir      string
		expectedPath string
		wantErr      bool
	}{
		{
			name: "Local Directory configs/assistant.yaml",
			files: []fileSetup{
				{path: "configs/assistant.yaml", content: "test content"},
			},
			expectedPath: "configs/assistant.yaml",
		},
		{
			name: "Local Directory assistant.yaml",
			files: []fileSetup{
				{path: "assistant.yaml", content: "test content"},
			},
			expectedPath: "assistant.yaml",
		},
		{
			name: "Parent Traversal",
			files: []fileSetup{
				{path: ".tell-me-go.yaml", content: "test content"},
			},
			baseDir:      "child",
			expectedPath: ".tell-me-go.yaml",
		},
		{
			name:         "Fallback",
			files:        []fileSetup{},
			expectedPath: "configs/assistant.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Setup files
			setupTestFS(t, tmpDir, tt.files)

			// Determine base directory for finder
			actualBaseDir := tmpDir
			if tt.baseDir != "" {
				actualBaseDir = filepath.Join(tmpDir, tt.baseDir)
				if err := os.MkdirAll(actualBaseDir, 0755); err != nil {
					t.Fatalf("failed to create baseDir: %v", err)
				}
			}

			finder := NewDefaultConfigFinder(WithBaseDir(actualBaseDir))
			path, err := finder.Find()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Find() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				expected := filepath.Join(tmpDir, tt.expectedPath)
				if path != expected {
					t.Errorf("got path %q; want %q", path, expected)
				}
			}
		})
	}
}
