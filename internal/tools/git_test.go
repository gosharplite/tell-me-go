// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	tmpDir := t.TempDir()
	
	runCmd := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = tmpDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("failed to run %s %v: %v", name, args, err)
		}
	}

	runCmd("git", "init")
	runCmd("git", "config", "user.email", "test@example.com")
	runCmd("git", "config", "user.name", "Test User")
	
	return tmpDir
}

func TestGitManager(t *testing.T) {
	tmpDir := setupGitRepo(t)
	
	// Change working directory to tmpDir for git commands
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	sm := NewSecurityManager()
	sm.bypassConfirmations = true // Enable bypass for tests

	m := &gitManager{sm: sm}
	ctx := context.Background()

	t.Run("getGitStatus", func(t *testing.T) {
		res, err := m.getGitStatus(ctx, nil)
		if err != nil {
			t.Fatalf("getGitStatus failed: %v", err)
		}
		// Empty repo status
		if res.Text != "" {
			t.Errorf("expected empty status, got %q", res.Text)
		}

		err = os.WriteFile("test.txt", []byte("hello"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		res, err = m.getGitStatus(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text == "" {
			t.Error("expected non-empty status after file creation")
		}
	})

	t.Run("getGitLog empty", func(t *testing.T) {
		_, err := m.getGitLog(ctx, map[string]interface{}{"limit": 10})
		if err == nil {
			t.Error("expected error for empty log")
		}
	})

	t.Run("gitCommit and getGitLog", func(t *testing.T) {
		// Stage file
		exec.Command("git", "add", "test.txt").Run()
		
		// In bypass mode, it should not ask for confirmation
		_, err := m.gitCommit(ctx, map[string]interface{}{"message": "initial commit"})
		if err != nil {
			t.Fatalf("gitCommit failed: %v", err)
		}
		
		logRes, err := m.getGitLog(ctx, map[string]interface{}{"limit": 1})
		if err != nil {
			t.Fatalf("getGitLog failed: %v", err)
		}
		if logRes.Text == "" {
			t.Error("expected non-empty log")
		}
	})
	
	t.Run("getGitDiff", func(t *testing.T) {
		os.WriteFile("test.txt", []byte("changed"), 0644)
		res, err := m.getGitDiff(ctx, map[string]interface{}{"staged": false})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text == "" {
			t.Error("expected diff")
		}
	})

	t.Run("getGitBlame", func(t *testing.T) {
		res, err := m.getGitBlame(ctx, map[string]interface{}{"filepath": "test.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text == "" {
			t.Error("expected blame output")
		}
	})

	t.Run("gitCreateBranch", func(t *testing.T) {
		res, err := m.gitCreateBranch(ctx, map[string]interface{}{"name": "new-branch"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text == "" {
			t.Error("expected output from branch creation")
		}
	})
}
