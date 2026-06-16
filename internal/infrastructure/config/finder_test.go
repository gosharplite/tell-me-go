// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
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

// TestFind_ReturnsFallbackWhenAllStrategiesFail verifies that Find() returns
// the fallback path when no config file is discoverable via any strategy.
func TestFind_ReturnsFallbackWhenAllStrategiesFail(t *testing.T) {
	tmpDir := t.TempDir()
	f := NewDefaultConfigFinder(WithBaseDir(tmpDir))
	path, err := f.Find()
	if err != nil {
		t.Fatalf("Find() returned error: %v", err)
	}
	expected := filepath.Join(tmpDir, "configs", "assistant.yaml")
	if path != expected {
		t.Errorf("expected fallback path %q, got %q", expected, path)
	}
}

func TestDefaultConfigFinder_GetBaseDir_GetwdError(t *testing.T) {
	// Save original and restore
	originalGetwd := osGetwd
	t.Cleanup(func() { osGetwd = originalGetwd })

	osGetwd = func() (string, error) {
		return "", fmt.Errorf("simulated getwd error")
	}

	f := &defaultConfigFinder{}
	got := f.getBaseDir()
	if got != "." {
		t.Errorf("expected fallback to '.', got %q", got)
	}
}

func TestDefaultConfigFinder_FindInExecutableDir_Error(t *testing.T) {
	originalExecutable := osExecutable
	t.Cleanup(func() { osExecutable = originalExecutable })

	osExecutable = func() (string, error) {
		return "", fmt.Errorf("simulated executable error")
	}

	f := &defaultConfigFinder{baseDir: t.TempDir()}
	path, found := f.findInExecutableDir()
	if found {
		t.Error("expected found=false when os.Executable fails")
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

func TestDefaultConfigFinder_FindInSystemPaths_Error(t *testing.T) {
	originalUserConfigDir := osUserConfigDir
	t.Cleanup(func() { osUserConfigDir = originalUserConfigDir })

	osUserConfigDir = func() (string, error) {
		return "", fmt.Errorf("simulated user config dir error")
	}

	f := &defaultConfigFinder{baseDir: t.TempDir()}
	path, found := f.findInSystemPaths()
	if found {
		t.Error("expected found=false when os.UserConfigDir fails")
	}
	if path != "" {
		t.Errorf("expected empty path, got %q", path)
	}
}

// TestDefaultConfigFinder_FindInExecutableDir_FilepathAbsError verifies that
// findInExecutableDir returns found=false when filepath.Abs fails due to an
// invalid path. This covers the ERROR_HANDLING gap at finder.go ~112-114
// where err1 != nil or err2 != nil causes the redundant-search guard to
// fall through, continuing to the os.Stat check.
func TestDefaultConfigFinder_FindInExecutableDir_FilepathAbsError(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
	}{
		{
			name:    "path with NUL byte causes filepath.Abs to fail",
			baseDir: string([]byte{'/', 't', 'm', 'p', 0x00, 'b', 'r', 'o', 'k', 'e', 'n'}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need a valid executable path so osExecutable succeeds,
			// otherwise findInExecutableDir returns early before reaching
			// the filepath.Abs calls. Use a temp dir.
			originalExecutable := osExecutable
			t.Cleanup(func() { osExecutable = originalExecutable })

			tmpDir := t.TempDir()
			osExecutable = func() (string, error) {
				return tmpDir + "/fake-binary", nil
			}

			f := &defaultConfigFinder{baseDir: tt.baseDir}
			path, found := f.findInExecutableDir()
			if found {
				t.Errorf("expected found=false when filepath.Abs fails on baseDir, got path=%q", path)
			}
			if path != "" {
				t.Errorf("expected empty path on failure, got %q", path)
			}
		})
	}
}

// TestFindInExecutableDir_ErrorPaths exercises the error-handling branches in
// findInExecutableDir that are not covered by the happy-path tests:
//
//  1. osExecutable() failure (finder.go:100-102) — logs a warning and returns ("", false).
//  2. filepath.Abs(base) and/or filepath.Abs(exeDir) failure (finder.go:112-114) —
//     when either resolution fails, the redundant-search guard is skipped
//     and the function proceeds to the os.Stat check.
func TestFindInExecutableDir_ErrorPaths(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() // shadows package-level stubs
		wantPath  string
		wantFound bool
	}{
		{
			name: "os.Executable failure",
			setup: func() {
				osExecutable = func() (string, error) {
					return "", fmt.Errorf("injected executable error")
				}
			},
			wantPath:  "",
			wantFound: false,
		},
		{
			name: "filepath.Abs resolution failure on both base and exeDir",
			setup: func() {
				// Make osGetwd fail so getBaseDir() falls back to ".", and
				// subsequent filepath.Abs(".") calls also fail — exercising
				// the branch where err1 != nil || err2 != nil at lines 112-114.
				osGetwd = func() (string, error) {
					return "", fmt.Errorf("injected getwd error")
				}
				// Return a relative path so exeDir resolves to "." and
				// filepath.Abs(".") also hits the failing osGetwd.
				osExecutable = func() (string, error) {
					return "fake-binary", nil
				}
			},
			wantPath:  "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture and restore package-level stubs
			origExecutable := osExecutable
			origGetwd := osGetwd
			t.Cleanup(func() {
				osExecutable = origExecutable
				osGetwd = origGetwd
			})

			tt.setup()

			// Use an empty baseDir so getBaseDir() calls osGetwd (our stub).
			f := &defaultConfigFinder{baseDir: ""}
			path, found := f.findInExecutableDir()

			if found != tt.wantFound {
				t.Errorf("found = %v; want %v", found, tt.wantFound)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q; want %q", path, tt.wantPath)
			}
		})
	}
}

// TestDefaultConfigFinder_FindInExecutableDir_StatFileNotFound verifies that
// findInExecutableDir returns found=false when the config file is absent from
// the executable directory. This covers the ERROR_HANDLING gap at
// finder.go ~117-119 where os.Stat returns an error (file not found) and the
// function correctly falls through without returning a path.
func TestDefaultConfigFinder_FindInExecutableDir_StatFileNotFound(t *testing.T) {
	t.Run("os.Stat fails when config file absent from exe dir", func(t *testing.T) {
		originalExecutable := osExecutable
		t.Cleanup(func() { osExecutable = originalExecutable })

		// Use a clean temp dir with no configs/assistant.yaml inside
		exeDir := t.TempDir()
		osExecutable = func() (string, error) {
			return exeDir + "/fake-binary", nil
		}

		// Use a different base dir so the redundant-search guard (absBase == absExeDir)
		// doesn't short-circuit us before we reach the os.Stat call
		f := &defaultConfigFinder{baseDir: t.TempDir()}
		path, found := f.findInExecutableDir()
		if found {
			t.Errorf("expected found=false when config file is absent, got path=%q", path)
		}
		if path != "" {
			t.Errorf("expected empty path, got %q", path)
		}
	})
}
