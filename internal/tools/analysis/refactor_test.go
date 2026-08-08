// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// atomicWriteErrorFS wraps a persistence.FileSystem and overrides
// AtomicWrite to return an error, simulating an atomic-write failure
// without OS-specific tricks like os.Chmod(0500).
type atomicWriteErrorFS struct {
	persistence.FileSystem
}

func (f *atomicWriteErrorFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return fs.ErrPermission
}

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
	_, err := tx.LoadFile(context.Background(), path)
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

func TestTransaction_LoadFile_Error(t *testing.T) {
	t.Parallel()
	tx := newTransaction()
	_, err := tx.LoadFile(context.Background(), "non_existent.go")
	if err == nil {
		t.Error("expected error loading non-existent file")
	}
}

func TestTransaction_LoadFile_CachedReturn(t *testing.T) {
	t.Parallel()
	tx := newTransaction()

	// First call: parses the file
	f1, err := tx.LoadFile(context.Background(), "testdata/valid.go")
	// If testdata/valid.go doesn't exist, create a temp file
	if err != nil {
		// Use a temp file instead
		tmpDir := t.TempDir()
		path := tmpDir + "/valid.go"
		require.NoError(t, os.WriteFile(path, []byte("package p\n\nfunc F() {}\n"), 0644))
		f1, err = tx.LoadFile(context.Background(), path)
		require.NoError(t, err)

		// Second call with same path: should return cached file
		f2, err := tx.LoadFile(context.Background(), path)
		require.NoError(t, err)
		if f1 != f2 {
			t.Error("LoadFile must return the same *ast.File pointer on cache hit")
		}
		return
	}
	require.NoError(t, err)

	// Second call with same path: should return cached file (same pointer)
	f2, err := tx.LoadFile(context.Background(), "testdata/valid.go")
	require.NoError(t, err)

	if f1 != f2 {
		t.Error("LoadFile must return the same *ast.File pointer on cache hit")
	}
}

func TestTransaction_Commit_ErrorPaths(t *testing.T) {
	// Subtest A: format.Node fails mid-loop → rollback cleans .tmp, original unchanged
	t.Run("format_Node_error_and_rollback", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		require.NoError(t, os.WriteFile(path, []byte("package p\n\nfunc F() {}\n"), 0644))

		tx := newTransaction()
		_, err := tx.LoadFile(context.Background(), path)
		require.NoError(t, err)

		// Corrupt AST so format.Node fails
		tx.Add(&mockTransform{
			applyFn: func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
				if f, ok := files[path]; ok {
					f.Name = nil // causes format.Node error
				}
				return nil
			},
		})

		err = tx.Commit(context.Background())
		require.Error(t, err)

		// No .tmp left (rollback cleaned)
		_, statErr := os.Stat(path + ".tmp")
		assert.True(t, os.IsNotExist(statErr))

		// Original unchanged
		data, _ := os.ReadFile(path)
		assert.Contains(t, string(data), "func F()")
	})

	// Subtest B: os.Rename fails → error returned, .tmp cleaned
	t.Run("rename_error", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("chmod-based rename failure not reliable on Windows")
		}
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		require.NoError(t, os.WriteFile(path, []byte("package p\n\nfunc F() {}\n"), 0644))

		tx := newTransaction()
		_, err := tx.LoadFile(context.Background(), path)
		require.NoError(t, err)

		tx.Add(&mockTransform{
			applyFn: func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
				return nil
			},
		})

		// Replace path with a directory so os.Rename fails
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.Mkdir(path, 0755))
		t.Cleanup(func() {
			if err := os.RemoveAll(path); err != nil {
				t.Logf("cleanup: remove %s: %v", path, err)
			}
		})

		err = tx.Commit(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to rename temp file")

		// .tmp cleaned (rollback)
		_, statErr := os.Stat(path + ".tmp")
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("transform_apply_error_is_wrapped", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		require.NoError(t, os.WriteFile(path, []byte("package p\n\nfunc F() {}\n"), 0644))

		tx := newTransaction()
		_, err := tx.LoadFile(context.Background(), path)
		require.NoError(t, err)

		tx.Add(&mockTransform{
			applyFn: func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
				return fmt.Errorf("simulated transform failure")
			},
		})

		err = tx.Commit(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transform 0")
		assert.Contains(t, err.Error(), "simulated transform failure")
	})

	t.Run("atomic_write_error_is_wrapped", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "test.go")
		content := []byte("package p\n\nfunc F() {}\n")

		// LoadFile reads through the injected FS, so the mock must hold
		// the source before the AtomicWrite failure is exercised.
		mfs := persistence.NewMockFileSystem()
		require.NoError(t, mfs.WriteFile(ctx, path, content, 0644))

		tx := newTransaction()
		tx.fs = &atomicWriteErrorFS{FileSystem: mfs}
		_, err := tx.LoadFile(ctx, path)
		require.NoError(t, err)

		tx.Add(&mockTransform{
			applyFn: func(ctx context.Context, fset *token.FileSet, files map[string]*ast.File) error {
				return nil
			},
		})

		err = tx.Commit(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "atomic write")
	})

	t.Run("load_file_parse_error_is_wrapped", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "broken.go")
		require.NoError(t, os.WriteFile(path, []byte("not valid go {{{"), 0644))

		tx := newTransaction()
		_, err := tx.LoadFile(context.Background(), path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
		assert.Contains(t, err.Error(), "broken.go")
	})
}
