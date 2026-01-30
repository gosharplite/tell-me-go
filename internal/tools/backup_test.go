// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupManager(t *testing.T) {
	sm := NewSecurityManager()
	bm := NewBackupManager(sm, 5)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")

	// 1. Snapshot non-existent file (creation)
	bm.Snapshot(filePath, "WRITE")
	if err := os.WriteFile(filePath, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Snapshot existing file (modification)
	bm.Snapshot(filePath, "REPLACE")
	if err := os.WriteFile(filePath, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Undo modification
	msg, err := bm.Undo(1)
	if err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	content, _ := os.ReadFile(filePath)
	if string(content) != "v1" {
		t.Errorf("Expected v1, got %s", string(content))
	}

	// 4. Undo creation
	msg, err = bm.Undo(1)
	if err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("Expected file to be removed, but it exists")
	}
	_ = msg
}
