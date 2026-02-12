// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	tests := []struct {
		name    string
		path    string
		data    []byte
		perm    os.FileMode
		wantErr bool
		setup   func(t *testing.T)
	}{
		{
			name:    "write new file",
			path:    filepath.Join(tmpDir, "new.txt"),
			data:    []byte("hello"),
			perm:    0644,
			wantErr: false,
		},
		{
			name:    "overwrite existing file",
			path:    filepath.Join(tmpDir, "existing.txt"),
			data:    []byte("initial"),
			perm:    0644,
			wantErr: false,
		},
		{
			name:    "write in subdirectory",
			path:    filepath.Join(tmpDir, "subdir", "file.txt"),
			data:    []byte("subdir"),
			perm:    0644,
			wantErr: false,
		},
		{
			name:    "invalid path - directory exists as file",
			path:    filepath.Join(tmpDir, "file_as_dir", "file.txt"),
			data:    []byte("error"),
			perm:    0644,
			wantErr: true,
			setup: func(t *testing.T) {
				if err := os.WriteFile(filepath.Join(tmpDir, "file_as_dir"), []byte("not a dir"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			if tt.name == "overwrite existing file" {
				err := os.WriteFile(tt.path, []byte("pre-existing"), 0644)
				if err != nil {
					t.Fatalf("failed to create pre-existing file: %v", err)
				}
			}

			err := AtomicWrite(ctx, tt.path, tt.data, tt.perm)
			if (err != nil) != tt.wantErr {
				t.Errorf("AtomicWrite() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				verifyFileState(t, tt.path, tt.data, tt.perm)
			}
		})
	}
}

func verifyFileState(t *testing.T, path string, expectedData []byte, expectedPerm os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("failed to read file: %v", err)
		return
	}
	if string(got) != string(expectedData) {
		t.Errorf("AtomicWrite() got = %q, want %q", string(got), string(expectedData))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("failed to stat file: %v", err)
		return
	}
	if info.Mode().Perm() != expectedPerm {
		t.Logf("AtomicWrite() perm got = %o, want %o (umask might affect this)", info.Mode().Perm(), expectedPerm)
	}
}

func TestAtomicWrite_CancellationCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cancel.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := AtomicWrite(ctx, path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	// Verify no temp file left behind
	matches, err := filepath.Glob(path + ".*.tmp")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	if len(matches) > 0 {
		t.Errorf("temp files %v still exist after cancellation", matches)
	}
}

func TestAtomicWrite_RenameFailure(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "target_dir")
	err := os.MkdirAll(path, 0755)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	err = AtomicWrite(ctx, path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error when renaming to a directory")
	}

	// Verify no temp file left behind
	matches, err := filepath.Glob(path + ".*.tmp")
	if err != nil {
		t.Errorf("Glob failed: %v", err)
	}
	if len(matches) > 0 {
		t.Errorf("temp files %v still exist after rename failure", matches)
	}
}

func TestAtomicWrite_OpenFileFailure(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a directory where we want to write, and make it read-only
	// to force os.CreateTemp to fail.
	subDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(subDir, "fail.txt")

	// Make subDir non-writable
	if err := os.Chmod(subDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(subDir, 0755); err != nil {
			t.Errorf("failed to restore permissions: %v", err)
		}
	}()

	ctx := context.Background()
	err := AtomicWrite(ctx, path, []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error when directory is not writable")
	}
}

func TestAtomicWrite_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cancel.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := AtomicWrite(ctx, path, []byte("data"), 0644)
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}
