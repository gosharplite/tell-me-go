// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestBackupManager_Undo(t *testing.T) {
	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	bm := newBackupManager(sm, infrapersistence.NewOSFileSystem(), 10)
	ctx := context.Background()

	path := filepath.Join(tempDir, "test.txt")

	// 1. Snapshot new file creation
	bm.snapshot(ctx, path, "WRITE")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Snapshot modification
	bm.snapshot(ctx, path, "REPLACE")
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
	setup         func(t *testing.T, tempDir string, sm *security.SecurityManager) func()
	snapshotPath  string
	snapshotOp    string
	wantErrSubstr string
	wantResSubstr string
}

func TestBackupManager_Undo_Errors(t *testing.T) {
	tests := []undoErrorTestCase{
		{
			name: "NoSnapshots",
			setup: func(t *testing.T, tempDir string, sm *security.SecurityManager) func() {
				return func() {}
			},
			wantResSubstr: "No snapshots available to undo.",
		},
		{
			name: "PermissionDenied",
			setup: func(t *testing.T, tempDir string, sm *security.SecurityManager) func() {
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
			setup: func(t *testing.T, tempDir string, sm *security.SecurityManager) func() {
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
		},
		{
			name: "AtomicWriteFailed",
			setup: func(t *testing.T, tempDir string, sm *security.SecurityManager) func() {
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
	sm := security.NewSecurityManager(nil)
	bm := newBackupManager(sm, infrapersistence.NewOSFileSystem(), 10)
	ctx := context.Background()

	cleanup := tc.setup(t, tempDir, sm)
	defer cleanup()

	fullPath := tc.snapshotPath
	if fullPath != "" {
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(tempDir, fullPath)
		}
		bm.snapshot(ctx, fullPath, tc.snapshotOp)
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
