// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertFileContent(t *testing.T, fs *MockFileSystem, ctx context.Context, path, expected string) {
	t.Helper()
	got, err := fs.ReadFile(ctx, path)
	if err != nil {
		t.Errorf("ReadFile(%q) failed: %v", path, err)
		return
	}
	if string(got) != expected {
		t.Errorf("ReadFile(%q) = %q, want %q", path, string(got), expected)
	}
}

func TestMockFileSystem(t *testing.T) {
	ctx := context.Background()

	t.Run("Write and Read", func(t *testing.T) {
		fs := NewMockFileSystem()
		path := "test.txt"
		data := "hello"
		_ = fs.WriteFile(ctx, path, []byte(data), 0644)
		assertFileContent(t, fs, ctx, path, data)
	})

	t.Run("ReadDir", func(t *testing.T) {
		fs := NewMockFileSystem()
		_ = fs.WriteFile(ctx, "a/b.txt", []byte("b"), 0644)
		_ = fs.WriteFile(ctx, "a/c/d.txt", []byte("d"), 0644)

		entries, err := fs.ReadDir(ctx, "a")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("Stat", func(t *testing.T) {
		fs := NewMockFileSystem()
		_ = fs.WriteFile(ctx, "stat.txt", []byte("abc"), 0644)
		info, err := fs.Stat(ctx, "stat.txt")
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 3 {
			t.Errorf("expected size 3, got %d", info.Size())
		}
		if info.IsDir() {
			t.Error("expected not a dir")
		}

		_ = fs.WriteFile(ctx, "a/b.txt", []byte("b"), 0644)
		info, err = fs.Stat(ctx, "a")
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Error("expected dir")
		}
	})

	t.Run("remove and removeAll", func(t *testing.T) {
		fs := NewMockFileSystem()
		_ = fs.WriteFile(ctx, "rem.txt", []byte("x"), 0644)
		_ = fs.remove(ctx, "rem.txt")
		if _, err := fs.ReadFile(ctx, "rem.txt"); err == nil {
			t.Error("expected rem.txt to be removed")
		}

		_ = fs.WriteFile(ctx, "dir/a.txt", []byte("a"), 0644)
		_ = fs.WriteFile(ctx, "dir/b.txt", []byte("b"), 0644)
		_ = fs.WriteFile(ctx, "other.txt", []byte("o"), 0644)
		_ = fs.removeAll(ctx, "dir")
		if len(fs.Files) != 1 {
			t.Errorf("expected 1 file left, got %d: %v", len(fs.Files), fs.Files)
		}
	})
}

func TestMockFileSystem_Walk(t *testing.T) {
	ctx := context.Background()
	fs := setupWalkFS(t, ctx)

	t.Run("Walk Basic", func(t *testing.T) {
		runWalkBasicTest(t, ctx, fs)
	})

	t.Run("Walk SkipDir", func(t *testing.T) {
		runWalkSkipDirTest(t, ctx, fs)
	})

	t.Run("Walk Root Dot", func(t *testing.T) {
		runWalkRootDotTest(t, ctx, fs)
	})
}

func setupWalkFS(t *testing.T, ctx context.Context) *MockFileSystem {
	fs := NewMockFileSystem()
	_ = fs.WriteFile(ctx, "root/a.txt", []byte("a"), 0644)
	_ = fs.WriteFile(ctx, "root/sub/b.txt", []byte("b"), 0644)
	_ = fs.WriteFile(ctx, "root/sub/c.txt", []byte("c"), 0644)
	_ = fs.WriteFile(ctx, "other/d.txt", []byte("d"), 0644)
	return fs
}

func runWalkBasicTest(t *testing.T, ctx context.Context, fs *MockFileSystem) {
	var seen []string
	err := fs.Walk(ctx, "root", func(path string, info os.FileInfo, err error) error {
		seen = append(seen, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{"root", "root/a.txt", "root/sub", "root/sub/b.txt", "root/sub/c.txt"}
	if len(seen) != len(expected) {
		t.Errorf("expected %d items, got %d: %v", len(expected), len(seen), seen)
	}
}

func runWalkSkipDirTest(t *testing.T, ctx context.Context, fs *MockFileSystem) {
	var seen []string
	err := fs.Walk(ctx, "root", func(path string, info os.FileInfo, err error) error {
		seen = append(seen, path)
		if path == "root/sub" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range seen {
		if strings.HasPrefix(p, "root/sub/") {
			t.Errorf("did not expect to see %s after SkipDir", p)
		}
	}
}

func runWalkRootDotTest(t *testing.T, ctx context.Context, fs *MockFileSystem) {
	var seen []string
	err := fs.Walk(ctx, ".", func(path string, info os.FileInfo, err error) error {
		seen = append(seen, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) < 4 {
		t.Errorf("expected many files when walking '.', got %v", seen)
	}
}
