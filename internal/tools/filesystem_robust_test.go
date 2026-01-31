// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

func TestFileSystemManagerRobust(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)
	m := &fileSystemManager{sm: sm, fs: fsutil.DefaultFileSystem}

	// Create test structure
	// tmpDir/
	//   file1.txt
	//   subdir/
	//     file2.go
	subdir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subdir, 0755)
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello world"), 0644)
	os.WriteFile(filepath.Join(subdir, "file2.go"), []byte("package main\nfunc main() {}"), 0644)

	ctx := context.Background()

	t.Run("getTree", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "max_depth": 2}
		res, err := m.getTree(ctx, args)
		if err != nil {
			t.Fatalf("getTree failed: %v", err)
		}
		if !strings.Contains(res.Text, "file1.txt") || !strings.Contains(res.Text, "subdir") || !strings.Contains(res.Text, "file2.go") {
			t.Errorf("getTree output missing expected files: %s", res.Text)
		}
	})

	t.Run("findFile", func(t *testing.T) {
		args := map[string]interface{}{"path": tmpDir, "pattern": "*.go"}
		res, err := m.findFile(ctx, args)
		if err != nil {
			t.Fatalf("findFile failed: %v", err)
		}
		if !strings.Contains(res.Text, "file2.go") {
			t.Errorf("findFile failed to find file2.go: %s", res.Text)
		}
	})

	t.Run("getFileDiff", func(t *testing.T) {
		f1 := filepath.Join(tmpDir, "diff1.txt")
		f2 := filepath.Join(tmpDir, "diff2.txt")
		os.WriteFile(f1, []byte("line1\nline2\n"), 0644)
		os.WriteFile(f2, []byte("line1\nline3\n"), 0644)

		args := map[string]interface{}{"file1": f1, "file2": f2}
		res, err := m.getFileDiff(ctx, args)
		if err != nil {
			t.Fatalf("getFileDiff failed: %v", err)
		}
		// Basic check for diff format
		if !strings.Contains(res.Text, "-line2") || !strings.Contains(res.Text, "+line3") {
			t.Errorf("getFileDiff output incorrect: %s", res.Text)
		}
	})

	t.Run("undoFileChange", func(t *testing.T) {
		bm := NewBackupManager(sm, 10)
		m.bm = bm // Initialize the backup manager in the manager
		args := map[string]interface{}{"n": 1}
		res, err := m.undoFileChange(ctx, args)
		if err != nil {
			t.Fatalf("undoFileChange failed: %v", err)
		}
		if !strings.Contains(res.Text, "No snapshots available") && !strings.Contains(res.Text, "No changes to revert") {
			t.Errorf("Expected 'No snapshots available' or 'No changes to revert', got: %s", res.Text)
		}
	})

	t.Run("grepDefinitions_and_getFileSkeleton_integration", func(t *testing.T) {
		// These tools in filesystem.go call grepDefinitionsGo/getFileSkeletonGo from intelligence.go
		// We already tested those, but let's verify the tool wrapper works.
		argsGrep := map[string]interface{}{"path": tmpDir}
		resGrep, err := m.grepDefinitions(ctx, argsGrep)
		if err != nil {
			t.Fatalf("grepDefinitions tool failed: %v", err)
		}
		if !strings.Contains(resGrep.Text, "func main()") {
			t.Errorf("grepDefinitions failed to find func main: %s", resGrep.Text)
		}

		argsSkel := map[string]interface{}{"filepath": filepath.Join(subdir, "file2.go")}
		resSkel, err := m.getFileSkeleton(ctx, argsSkel)
		if err != nil {
			t.Fatalf("getFileSkeleton tool failed: %v", err)
		}
		if !strings.Contains(resSkel.Text, "func main()") {
			t.Errorf("getFileSkeleton failed: %s", resSkel.Text)
		}
	})
}
