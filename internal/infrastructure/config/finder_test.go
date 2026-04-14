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

func TestDefaultConfigFinder_GetBaseDir(t *testing.T) {
	t.Run("with baseDir", func(t *testing.T) {
		f := &defaultConfigFinder{baseDir: "/custom/path"}
		if got := f.getBaseDir(); got != "/custom/path" {
			t.Errorf("getBaseDir() = %v; want /custom/path", got)
		}
	})

	t.Run("without baseDir", func(t *testing.T) {
		f := &defaultConfigFinder{}
		got := f.getBaseDir()
		wd, _ := os.Getwd()
		if got != wd && got != "." {
			t.Errorf("getBaseDir() = %v; want %v or \".\"", got, wd)
		}
	})
}

func TestDefaultConfigFinder_FindInSystemPaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Mock UserConfigDir via environment variables
	// os.UserConfigDir() on Unix uses XDG_CONFIG_HOME
	// On Windows it uses AppData
	// On Darwin it uses HOME + /Library/Application Support
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("AppData", tmpDir)
	t.Setenv("HOME", tmpDir)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("UserConfigDir not available")
	}

	appDir := filepath.Join(configDir, "tell-me-go")
	configPath := filepath.Join(appDir, "assistant.yaml")

	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("system config"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Use a clean baseDir that doesn't contain any local config
	emptyBase := t.TempDir()
	finder := NewDefaultConfigFinder(WithBaseDir(emptyBase))
	path, err := finder.Find()
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}

	if path != configPath {
		t.Errorf("got path %q; want %q", path, configPath)
	}
}

func TestDefaultConfigFinder_FindInExecutableDir(t *testing.T) {
	// Exercise findInExecutableDir even if it returns false
	f := &defaultConfigFinder{baseDir: t.TempDir()}
	_, found := f.findInExecutableDir()
	// We don't necessarily expect it to be found since we can't easily put
	// a config file next to the test binary without knowing its location,
	// but calling it ensures coverage.
	_ = found
}
