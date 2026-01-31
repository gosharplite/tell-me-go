// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
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
	os.WriteFile(path, []byte("v1"), 0644)

	// 2. Snapshot modification
	bm.Snapshot(path, "REPLACE")
	os.WriteFile(path, []byte("v2"), 0644)

	// 3. Undo modification
	res, err := bm.Undo(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "Restored") {
		t.Errorf("expected Restored, got %s", res)
	}
	content, _ := os.ReadFile(path)
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
