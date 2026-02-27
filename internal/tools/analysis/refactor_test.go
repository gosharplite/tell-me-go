// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

type mockTransform struct {
	applyFn func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error
}

func (m *mockTransform) Apply(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
	return m.applyFn(ctx, fset, files)
}

func TestTransaction_Commit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.go")
	content := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tx := newTransaction()
	_, err := tx.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	// Add a transform that does something
	tx.Add(&mockTransform{
		applyFn: func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
			// Just a no-op that succeeds
			return nil
		},
	})

	err = tx.Commit(context.Background())
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify file still exists and has content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path for target, got: %s", path)
	}
	if len(data) == 0 {
		t.Error("file is empty after commit")
	}
}

func TestTransaction_Rollback(t *testing.T) {
	t.Parallel()
	tx := newTransaction()
	path := "test_rollback.txt"
	_ = os.WriteFile(path+".tmp", []byte("temp"), 0644)
	defer os.Remove(path + ".tmp")

	tx.rollback([]string{path})

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected temp file to be removed after rollback")
	}
}

func TestTransaction_LoadFile_Error(t *testing.T) {
	t.Parallel()
	tx := newTransaction()
	_, err := tx.LoadFile("non_existent.go")
	if err == nil {
		t.Error("expected error loading non-existent file")
	}
}
