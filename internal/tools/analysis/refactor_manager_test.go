// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type MockSecurityProvider struct {
	security.SecurityProvider
	IsPathWritableFunc           func(path string) (string, error)
	ConfirmDestructiveActionFunc func(ctx context.Context, action, target, detail string) (bool, error)
	IsPathSafeFunc               func(path string) (string, error)
}

func (m *MockSecurityProvider) IsPathWritable(path string) (string, error) {
	if m.IsPathWritableFunc != nil {
		return m.IsPathWritableFunc(path)
	}
	return path, nil
}

func (m *MockSecurityProvider) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	if m.ConfirmDestructiveActionFunc != nil {
		return m.ConfirmDestructiveActionFunc(ctx, action, target, detail)
	}
	return true, nil
}

func (m *MockSecurityProvider) IsPathSafe(path string) (string, error) {
	if m.IsPathSafeFunc != nil {
		return m.IsPathSafeFunc(path)
	}
	return path, nil
}

func TestMoveDefinition(t *testing.T) {
	ctx := context.Background()

	t.Run("Action Denied", func(t *testing.T) {
		sp := &MockSecurityProvider{
			ConfirmDestructiveActionFunc: func(ctx context.Context, action, target, detail string) (bool, error) {
				return false, nil
			},
		}
		mgr := newRefactorManager(sp)

		args := map[string]interface{}{
			"symbol":   "MyFunc",
			"src_file": "src.go",
			"dst_file": "dst.go",
			"reason":   "testing",
		}

		res, err := mgr.MoveDefinition(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Text != "Action denied by user." {
			t.Errorf("expected denial message, got %q", res.Text)
		}
	})

	t.Run("IsPathWritable error", func(t *testing.T) {
		sp := &MockSecurityProvider{
			IsPathWritableFunc: func(path string) (string, error) {
				return "", fmt.Errorf("error")
			},
		}
		mgr := newRefactorManager(sp)
		args := map[string]interface{}{
			"symbol":   "MyFunc",
			"src_file": "src.go",
			"dst_file": "dst.go",
			"reason":   "testing",
		}
		_, err := mgr.MoveDefinition(ctx, args)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("Successful Move", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "refactor-test")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		srcPath := filepath.Join(tmpDir, "src.go")
		dstPath := filepath.Join(tmpDir, "dst.go")

		err = os.WriteFile(srcPath, []byte("package test\n\nfunc MyFunc() {}\n"), 0644)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(dstPath, []byte("package test\n"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		sp := &MockSecurityProvider{}
		mgr := newRefactorManager(sp)

		args := map[string]interface{}{
			"symbol":   "MyFunc",
			"src_file": srcPath,
			"dst_file": dstPath,
			"reason":   "testing",
		}

		res, err := mgr.MoveDefinition(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Text == "" {
			t.Error("expected success message, got empty")
		}

		// Verify content
		srcContent, _ := os.ReadFile(srcPath)
		dstContent, _ := os.ReadFile(dstPath)

		if string(srcContent) == "package test\n\nfunc MyFunc() {}\n" {
			t.Error("src file should have changed")
		}
		if string(dstContent) == "package test\n" {
			t.Error("dst file should have changed")
		}
	})
}

func TestRenameSymbol(t *testing.T) {
	ctx := context.Background()

	t.Run("Action Denied", func(t *testing.T) {
		sp := &MockSecurityProvider{
			ConfirmDestructiveActionFunc: func(ctx context.Context, action, target, detail string) (bool, error) {
				return false, nil
			},
		}
		mgr := newRefactorManager(sp)

		args := map[string]interface{}{
			"old_name": "Old",
			"new_name": "New",
			"path":     ".",
			"reason":   "testing",
		}

		res, err := mgr.RenameSymbol(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Text != "Action denied by user." {
			t.Errorf("expected denial message, got %q", res.Text)
		}
	})

	t.Run("Successful Orchestration", func(t *testing.T) {
		sp := &MockSecurityProvider{}
		mgr := newRefactorManager(sp)

		args := map[string]interface{}{
			"old_name": "Old",
			"new_name": "New",
			"path":     ".",
			"reason":   "testing",
		}

		res, err := mgr.RenameSymbol(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "RenameSymbol migrated") {
			t.Errorf("expected migration message, got %q", res.Text)
		}
	})
}
