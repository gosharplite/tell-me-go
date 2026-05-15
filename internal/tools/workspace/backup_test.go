// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestBackupManager_Undo(t *testing.T) {
	tempDir := t.TempDir()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.RegisterSafePath(tempDir)
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10)
	ctx := context.Background()

	path := filepath.Join(tempDir, "test.txt")

	// 1. Snapshot new file creation
	if err := bm.snapshot(ctx, path, "WRITE"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Snapshot modification
	if err := bm.snapshot(ctx, path, "REPLACE"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Undo modification
	res, err := bm.undo(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "Restored") {
		t.Errorf("expected Restored, got %s", res)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "v1" {
		t.Errorf("got %s, want v1", content)
	}

	// 4. Undo creation
	res, err = bm.undo(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "Removed") {
		t.Errorf("expected Removed, got %s", res)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}
}

type undoErrorTestCase struct {
	name          string
	setup         func(t *testing.T, tempDir string, sm *toolstest.MockSecurityManager) func()
	snapshotPath  string
	snapshotOp    string
	wantErrSubstr string
	wantResSubstr string
	mockFS        persistence.FileSystem // optional: override FS for snapshot (e.g. to simulate ReadFile returning os.ErrNotExist)
}

func TestBackupManager_Undo_Errors(t *testing.T) {
	tests := []undoErrorTestCase{
		{
			name: "NoSnapshots",
			setup: func(t *testing.T, tempDir string, sm *toolstest.MockSecurityManager) func() {
				return func() {}
			},
			wantResSubstr: "No snapshots available to undo.",
		},
		{
			name: "PermissionDenied",
			setup: func(t *testing.T, tempDir string, sm *toolstest.MockSecurityManager) func() {
				sm.AllowAll = false
				sm.IsWritableFunc = func(path string) (string, error) {
					return "", errors.New("security violation")
				}
				return func() {}
			},
			snapshotPath: func() string {
				if runtime.GOOS == "windows" {
					return `C:\unauthorized-path-for-test\denied.txt`
				}
				return "/unauthorized-path-for-test/denied.txt"
			}(),
			snapshotOp:    "WRITE",
			wantErrSubstr: "security violation",
		},
		{
			name: "RemoveFailed",
			setup: func(t *testing.T, tempDir string, sm *toolstest.MockSecurityManager) func() {
				sm.RegisterSafePath(tempDir)
				dirPath := filepath.Join(tempDir, "is_a_dir")
				if err := os.Mkdir(dirPath, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dirPath, "file"), []byte("data"), 0644); err != nil {
					t.Fatal(err)
				}
				return func() {}
			},
			snapshotPath:  "is_a_dir",
			snapshotOp:    "WRITE",
			wantErrSubstr: "failed to remove new file",
			// Mock FS reports os.ErrNotExist for ReadFile so snapshot stores nil content,
			// causing undoOne to call Remove on the non-empty directory which fails.
			mockFS: &mockFS_NotExist{FileSystem: persistencetest.NewPlainOSFileSystem()},
		},
		{
			name: "AtomicWriteFailed",
			setup: func(t *testing.T, tempDir string, sm *toolstest.MockSecurityManager) func() {
				sm.RegisterSafePath(tempDir)
				path := filepath.Join(tempDir, "readonly.txt")
				if err := os.WriteFile(path, []byte("initial"), 0644); err != nil {
					t.Fatal(err)
				}

				// On Windows, Chmod 0444 on the file is more reliable.
				// On POSIX, we must make the directory read-only to prevent Rename/CreateTemp.
				if runtime.GOOS == "windows" {
					if err := os.Chmod(path, 0444); err != nil {
						t.Fatal(err)
					}
					return func() {
						_ = os.Chmod(path, 0644)
					}
				} else {
					if err := os.Chmod(tempDir, 0555); err != nil {
						t.Fatal(err)
					}
					return func() {
						_ = os.Chmod(tempDir, 0755)
					}
				}
			},
			snapshotPath:  "readonly.txt",
			snapshotOp:    "REPLACE",
			wantErrSubstr: "failed to restore",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runUndoErrorTest(t, tc)
		})
	}
}

func runUndoErrorTest(t *testing.T, tc undoErrorTestCase) {
	tempDir := t.TempDir()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := tc.mockFS
	if fs == nil {
		fs = persistencetest.NewPlainOSFileSystem()
	}
	bm := newBackupManager(sm, fs, 10)
	ctx := context.Background()

	cleanup := tc.setup(t, tempDir, sm)
	defer cleanup()

	fullPath := tc.snapshotPath
	if fullPath != "" {
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(tempDir, fullPath)
		}
		if err := bm.snapshot(ctx, fullPath, tc.snapshotOp); err != nil {
			t.Fatalf("unexpected snapshot error: %v", err)
		}
	}

	res, err := bm.undo(ctx, 1)

	if tc.wantErrSubstr != "" {
		if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
			t.Errorf("expected error containing %q, got %v", tc.wantErrSubstr, err)
		}
		return
	}

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tc.wantResSubstr != "" && !strings.Contains(res, tc.wantResSubstr) {
		t.Errorf("expected result containing %q, got %q", tc.wantResSubstr, res)
	}
}

// ---------------------------------------------------------------------------
// newBackupManager normalization tests
// ---------------------------------------------------------------------------

func TestNewBackupManager_Normalization(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := persistencetest.NewPlainOSFileSystem()

	t.Run("zero normalized", func(t *testing.T) {
		bm := newBackupManager(sm, fs, 0)
		if bm.maxStored != 10 {
			t.Errorf("expected maxStored=10 for input 0, got %d", bm.maxStored)
		}
	})

	t.Run("negative normalized", func(t *testing.T) {
		bm := newBackupManager(sm, fs, -5)
		if bm.maxStored != 10 {
			t.Errorf("expected maxStored=10 for input -5, got %d", bm.maxStored)
		}
	})

	t.Run("positive preserved", func(t *testing.T) {
		bm := newBackupManager(sm, fs, 3)
		if bm.maxStored != 3 {
			t.Errorf("expected maxStored=3 for input 3, got %d", bm.maxStored)
		}
	})
}

// mockFS_NotExist is a FileSystem decorator that returns os.ErrNotExist
// from ReadFile. Used to test undo paths that need a nil-content snapshot
// on a path that actually exists as a directory.
type mockFS_NotExist struct {
	persistence.FileSystem
}

func (m *mockFS_NotExist) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return nil, os.ErrNotExist
}

// ---------------------------------------------------------------------------
// snapshot error path tests (Phase 3+4)
// ---------------------------------------------------------------------------

// mockFS_ReadError is a FileSystem decorator that returns a non-IsNotExist
// error from ReadFile.
type mockFS_ReadError struct {
	persistence.FileSystem
	err error
}

func (m *mockFS_ReadError) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return nil, m.err
}

func TestSnapshot_ReadFileIOError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	expectedErr := errors.New("disk failure")
	fs := &mockFS_ReadError{FileSystem: persistencetest.NewPlainOSFileSystem(), err: expectedErr}
	bm := newBackupManager(sm, fs, 10)
	ctx := context.Background()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")

	err := bm.snapshot(ctx, path, "WRITE")
	if err == nil {
		t.Fatal("expected error from ReadFile I/O failure")
	}
	if !strings.Contains(err.Error(), "snapshot: read") {
		t.Errorf("expected 'snapshot: read' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "disk failure") {
		t.Errorf("expected 'disk failure' in error, got %q", err.Error())
	}
}

func TestSnapshot_ReadFileNotExist(t *testing.T) {
	// Verifies os.ErrNotExist is treated as new-file (nil content, no error)
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10)
	ctx := context.Background()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "nonexistent.txt")

	err := bm.snapshot(ctx, path, "WRITE")
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}

	// Verify a snapshot was stored
	res, undoErr := bm.undo(ctx, 1)
	if undoErr != nil {
		t.Fatalf("unexpected undo error: %v", undoErr)
	}
	if !strings.Contains(res, "Removed") {
		t.Errorf("expected 'Removed' for new-file undo, got %q", res)
	}
}

func TestSnapshot_RingBufferEviction(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	maxStored := 3
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), maxStored)
	ctx := context.Background()

	tempDir := t.TempDir()

	// Create 5 snapshots (exceeds maxStored=3)
	for i := 0; i < 5; i++ {
		path := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("v%d", i)), 0644); err != nil {
			t.Fatal(err)
		}
		if err := bm.snapshot(ctx, path, "WRITE"); err != nil {
			t.Fatal(err)
		}
	}

	// undo with n=3 should restore the 3 most recent files
	res, err := bm.undo(ctx, 3)
	if err != nil {
		t.Fatalf("unexpected undo error: %v", err)
	}
	if !strings.Contains(res, "Undo successful") {
		t.Errorf("expected successful undo, got %q", res)
	}

	// After undoing 3, there should be 0 left (since 5 snapshots - 3 undone = 2,
	// but ring-buffer evicted the first 2, so only 3 were stored)
	// Actually, we stored 5 but maxStored=3 so only last 3 kept. Undo 3 leaves 0.
	_, err = bm.undo(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected undo error: %v", err)
	}
	// Third undo should show "No snapshots available"
	res, err = bm.undo(ctx, 1)
	if err != nil {
		t.Fatalf("expected no error for empty undo, got: %v", err)
	}
	if !strings.Contains(res, "No snapshots available") {
		t.Errorf("expected 'No snapshots available', got %q", res)
	}
}

func TestUndo_NExceedingSnapshotCount(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10)
	ctx := context.Background()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "single.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := bm.snapshot(ctx, path, "WRITE"); err != nil {
		t.Fatal(err)
	}

	// Request more undos than available snapshots
	res, err := bm.undo(ctx, 5)
	if err != nil {
		t.Fatalf("unexpected undo error: %v", err)
	}
	if !strings.Contains(res, "Undo successful") {
		t.Errorf("expected 'Undo successful', got %q", res)
	}

	// All snapshots should be consumed
	res, err = bm.undo(ctx, 1)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(res, "No snapshots available") {
		t.Errorf("expected 'No snapshots available', got %q", res)
	}
}

// ---------------------------------------------------------------------------
// snapshot path resolution error path (Phase A, Task 2)
// ---------------------------------------------------------------------------

func TestSnapshot_WorkingDirectoryRemoved(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10)
	ctx := context.Background()

	// Create a temp dir, chdir into it, then remove it
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}
	// Remove the directory we're in — this causes os.Getwd() to fail on next call
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})
	if err := os.Remove(subDir); err != nil {
		// On some OSes, you can't remove the current working directory
		t.Skipf("cannot remove current working directory on this OS: %v", err)
	}
	// On some OSes (macOS), removing the CWD succeeds but os.Getwd still
	// resolves because the kernel holds a VFS reference to the unlinked
	// directory. Skip if Getwd still works — the error path is not testable.
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd still succeeds after CWD removal on this OS; error path not reachable")
	}

	// Now filepath.Abs should fail because os.Getwd fails
	err = bm.snapshot(ctx, "test.txt", "WRITE")
	if err == nil {
		t.Fatal("expected error from filepath.Abs when working directory is removed")
	}
	if !strings.Contains(err.Error(), "snapshot: resolve path") {
		t.Errorf("expected 'snapshot: resolve path' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// undo N normalization tests (Phase A, Task 2)
// ---------------------------------------------------------------------------

func TestUndo_NZeroNormalized(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10)
	ctx := context.Background()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := bm.snapshot(ctx, path, "WRITE"); err != nil {
		t.Fatal(err)
	}

	// n=0 should be normalized to 1
	res, err := bm.undo(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res, "Undo successful") {
		t.Errorf("expected successful undo, got: %q", res)
	}
}

func TestUndo_NegativeNNormalized(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	bm := newBackupManager(sm, persistencetest.NewPlainOSFileSystem(), 10)
	ctx := context.Background()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := bm.snapshot(ctx, path, "WRITE"); err != nil {
		t.Fatal(err)
	}

	// n=-1 should be normalized to 1
	res, err := bm.undo(ctx, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res, "Undo successful") {
		t.Errorf("expected successful undo, got: %q", res)
	}
}
