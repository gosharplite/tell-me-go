// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceText_Uniqueness(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tell-me-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")
	content := "line 1\ntarget\nline 3\ntarget\nline 5"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	sm.bypassConfirmations = true // Avoid interactive prompts

	m := &fileSystemManager{sm: sm, bm: NewBackupManager(sm, 1)}
	ctx := context.Background()

	// 1. Test failure when old_text appears multiple times
	args := map[string]interface{}{
		"filepath": filePath,
		"old_text": "target",
		"new_text": "replaced",
	}
	_, err = m.replaceText(ctx, args)
	if err == nil {
		t.Error("expected error when old_text is not unique, got nil")
	} else if !strings.Contains(err.Error(), "found 2 times") {
		t.Errorf("expected 'found 2 times' error, got: %v", err)
	}

	// 2. Test success when old_text is unique (with more context)
	args["old_text"] = "line 1\ntarget"
	_, err = m.replaceText(ctx, args)
	if err != nil {
		t.Errorf("expected success with context, got error: %v", err)
	}

	// Verify content
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "replaced\nline 3") {
		t.Errorf("content mismatch after replacement: %s", string(data))
	}
}
