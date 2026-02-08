// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestBackupManager_Undo(t *testing.T) {
	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	bm := NewBackupManager(sm, 10)
	ctx := context.Background()

	path := filepath.Join(tempDir, "test.txt")

	// 1. Snapshot new file creation
	bm.Snapshot(path, "WRITE")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Snapshot modification
	bm.Snapshot(path, "REPLACE")
	if err := os.WriteFile(path, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Undo modification
	res, err := bm.Undo(ctx, 1)
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
	res, err = bm.Undo(ctx, 1)
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

func TestBackupManager_Undo_Errors(t *testing.T) {
	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	bm := NewBackupManager(sm, 10)
	ctx := context.Background()

	t.Run("NoSnapshots", func(t *testing.T) {
		res, err := bm.Undo(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if res != "No snapshots available to undo." {
			t.Errorf("expected no snapshots message, got %s", res)
		}
	})

	t.Run("PermissionDenied", func(t *testing.T) {
		// Use a path that is NOT in CWD or TempDir to trigger security violation
		path := "/unauthorized-path-for-test/denied.txt"
		// Snapshot it (it will resolve Abs, but BackupManager doesn't check security on Snapshot)
		bm.Snapshot(path, "WRITE")
		// SecurityManager will deny this because it's not in CWD or TempDir
		_, err := bm.Undo(ctx, 1)
		if err == nil {
			t.Error("expected error due to permission denied, got nil")
		} else if !strings.Contains(err.Error(), "security violation") {
			t.Errorf("expected security violation error, got %v", err)
		}
	})

	t.Run("RemoveFailed", func(t *testing.T) {
		sm.RegisterSafePath(tempDir)
		// Create a directory where the file should be
		dirPath := filepath.Join(tempDir, "is_a_dir")
		if err := os.Mkdir(dirPath, 0755); err != nil {
			t.Fatal(err)
		}

		bm.Snapshot(dirPath, "WRITE")
		// os.Remove on a non-empty directory or just a directory might fail depending on how it's called
		// but here we just want to trigger an error in os.Remove.
		// Actually, os.Remove works on empty dirs. Let's put a file in it.
		if err := os.WriteFile(filepath.Join(dirPath, "file"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := bm.Undo(ctx, 1)
		if err == nil {
			t.Error("expected error removing non-empty directory, got nil")
		}
		if !strings.Contains(err.Error(), "failed to remove new file") {
			t.Errorf("expected remove error, got %v", err)
		}
	})

	t.Run("AtomicWriteFailed", func(t *testing.T) {
		path := filepath.Join(tempDir, "readonly.txt")
		if err := os.WriteFile(path, []byte("initial"), 0644); err != nil {
			t.Fatal(err)
		}

		bm.Snapshot(path, "REPLACE")

		// Make the directory read-only to make AtomicWrite fail (it creates a temp file)
		if err := os.Chmod(tempDir, 0555); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(tempDir, 0755) }()

		_, err := bm.Undo(ctx, 1)
		if err == nil {
			t.Error("expected error during atomic write, got nil")
		}
		if !strings.Contains(err.Error(), "failed to restore") {
			t.Errorf("expected restore error, got %v", err)
		}
	})
}
