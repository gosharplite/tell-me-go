// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProcessExecutor_AtomicWrites(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "atomic_test.txt")
	executor := newprocessExecutor()

	lineCount := 100
	config := executionConfig{
		OutputFile: tmpFile,
	}

	_, err := executor.RunCommand(context.Background(), []string{helperPath, "multi-line", fmt.Sprintf("%d", lineCount)}, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	actualLines := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		actualLines++
		if !strings.HasPrefix(line, "STDOUT_LINE_") && !strings.HasPrefix(line, "STDERR_LINE_") {
			t.Errorf("Detected interleaved or corrupted line: %q", line)
		}
	}

	expectedLines := 2 * lineCount
	if actualLines != expectedLines {
		t.Errorf("Expected %d lines in output file, got %d", expectedLines, actualLines)
	}
}

func TestOpenOutputFile_Security(t *testing.T) {
	executor := newprocessExecutor()
	tmpDir := setupSecurityTest(t)

	type testCase struct {
		name       string
		path       string
		append     bool
		wantErr    bool
		errContain string
	}

	tests := []testCase{
		{
			name:       "relative up",
			path:       "../../outside.txt",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:    "relative same level",
			path:    "inside.txt",
			wantErr: false,
		},
		{
			name:    "relative subdir",
			path:    "logs/test.log",
			wantErr: false,
		},
		{
			name:    "absolute path",
			path:    filepath.Join(tmpDir, "absolute.txt"),
			wantErr: false,
		},
		{
			name:       "nested relative up",
			path:       "logs/../../outside.txt",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:    "append mode",
			path:    "append.txt",
			append:  true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := executionConfig{
				OutputFile: tt.path,
				Append:     tt.append,
			}
			f, err := executor.openOutputFile(config)
			validateOpenResult(t, f, err, tt.wantErr, tt.errContain)
		})
	}
}

func setupSecurityTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})
	return tmpDir
}

func validateOpenResult(t *testing.T, f *os.File, err error, wantErr bool, errContain string) {
	t.Helper()
	if f != nil {
		defer func() { _ = f.Close() }()
	}

	if (err != nil) != wantErr {
		t.Errorf("error = %v, wantErr %v", err, wantErr)
		return
	}
	if wantErr && errContain != "" && (err == nil || !strings.Contains(err.Error(), errContain)) {
		t.Errorf("expected error containing %q, got %v", errContain, err)
	}
}

func TestResolveAndValidateOutputPath_Escape(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantErr    bool
		errContain string
	}{
		{
			name:       "bare dot-dot",
			path:       "..",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:       "dot-dot prefix",
			path:       "../etc/passwd",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:       "deep escape",
			path:       "a/b/../../../etc/passwd",
			wantErr:    true,
			errContain: "cannot escape current directory",
		},
		{
			name:    "normal subdirectory",
			path:    "a/b/c",
			wantErr: false,
		},
		{
			name:    "dot prefixed",
			path:    "./foo/bar",
			wantErr: false,
		},
		{
			name:    "absolute path",
			path:    "/tmp/foo",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absPath, err := resolveAndValidateOutputPath(filepath.Clean(tt.path), tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContain)
				} else if !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("expected error containing %q, got: %v", tt.errContain, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !filepath.IsAbs(absPath) {
				t.Errorf("expected absolute path, got %q", absPath)
			}
		})
	}
}

func TestOpenOutputFile_Sanitization(t *testing.T) {
	executor := newprocessExecutor()

	tests := []struct {
		name     string
		path     string
		expected string // partial match of the actual cleaned path
	}{
		{"trim whitespace", "  out.txt  ", "out.txt"},
		{"null bytes", "out" + string([]byte{0}) + ".txt", "out.txt"},
		{"mixed", "  logs/test" + string([]byte{0}) + ".log  ", "logs/test.log"},
		{"only nulls and spaces", "  \x00\x00  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := executionConfig{
				OutputFile: tt.path,
			}
			f, err := executor.openOutputFile(config)
			if err != nil {
				t.Fatalf("openOutputFile(%q) error = %v", tt.path, err)
			}
			if tt.expected == "" {
				if f != nil {
					_ = f.Close()
					t.Errorf("expected nil file for empty path after sanitization, got %q", f.Name())
				}
				return
			}
			if f != nil {
				name := f.Name()
				_ = f.Close()
				t.Cleanup(func() { _ = os.Remove(name) })

				// CRITICAL: Handle OS-specific separators in the expected substring
				expectedPath := filepath.FromSlash(tt.expected)
				if !strings.Contains(name, expectedPath) {
					t.Errorf("expected path to contain %q, got %q", expectedPath, name)
				}
				// Verify no spaces and no null bytes in the final base name
				base := filepath.Base(name)
				if strings.Contains(base, " ") || strings.Contains(base, "\x00") {
					t.Errorf("path %q still contains spaces or null bytes: %q", tt.path, base)
				}
			} else {
				t.Errorf("expected file object, got nil")
			}
		})
	}
}

func TestOpenOutputFile_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent MkdirAll on Windows")
	}

	executor := newprocessExecutor()
	tmpDir := t.TempDir()

	// Create a read-only parent directory so MkdirAll fails
	parentDir := filepath.Join(tmpDir, "readonly_parent")
	if err := os.MkdirAll(parentDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parentDir, 0755) })

	config := executionConfig{
		OutputFile: filepath.Join(parentDir, "child", "out.txt"),
	}
	f, err := executor.openOutputFile(config)
	if f != nil {
		_ = f.Close()
	}
	if err == nil {
		t.Fatal("expected error from MkdirAll under read-only parent")
	}
	if !strings.Contains(err.Error(), "failed to create output directory") {
		t.Errorf("expected 'failed to create output directory' in error, got: %v", err)
	}

}
