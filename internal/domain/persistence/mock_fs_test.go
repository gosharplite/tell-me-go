// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertFileContent(t *testing.T, fs *mockFileSystem, ctx context.Context, path, expected string) {
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
	t.Parallel()
	ctx := context.Background()

	t.Run("Write and Read", func(t *testing.T) {
		t.Parallel()
		fs := NewMockFileSystem()
		path := "test.txt"
		data := "hello"
		_ = fs.WriteFile(ctx, path, []byte(data), 0644)
		assertFileContent(t, fs, ctx, path, data)
	})

	t.Run("ReadDir", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
		fs := NewMockFileSystem()
		_ = fs.WriteFile(ctx, "rem.txt", []byte("x"), 0644)
		_ = fs.Remove(ctx, "rem.txt")
		if _, err := fs.ReadFile(ctx, "rem.txt"); err == nil {
			t.Error("expected rem.txt to be removed")
		}

		_ = fs.WriteFile(ctx, "dir/a.txt", []byte("a"), 0644)
		_ = fs.WriteFile(ctx, "dir/b.txt", []byte("b"), 0644)
		_ = fs.WriteFile(ctx, "other.txt", []byte("o"), 0644)
		_ = fs.RemoveAll(ctx, "dir")
		if len(fs.Files) != 1 {
			t.Errorf("expected 1 file left, got %d: %v", len(fs.Files), fs.Files)
		}
	})
}

func TestMockFileSystem_Walk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := setupWalkFS(t, ctx)

	t.Run("Walk Basic", func(t *testing.T) {
		t.Parallel()
		runWalkBasicTest(t, ctx, fs)
	})

	t.Run("Walk SkipDir", func(t *testing.T) {
		t.Parallel()
		runWalkSkipDirTest(t, ctx, fs)
	})

	t.Run("Walk Root Dot", func(t *testing.T) {
		t.Parallel()
		runWalkRootDotTest(t, ctx, fs)
	})
}

func setupWalkFS(t *testing.T, ctx context.Context) *mockFileSystem {
	fs := NewMockFileSystem()
	_ = fs.WriteFile(ctx, "root/a.txt", []byte("a"), 0644)
	_ = fs.WriteFile(ctx, "root/sub/b.txt", []byte("b"), 0644)
	_ = fs.WriteFile(ctx, "root/sub/c.txt", []byte("c"), 0644)
	_ = fs.WriteFile(ctx, "other/d.txt", []byte("d"), 0644)
	return fs
}

func runWalkBasicTest(t *testing.T, ctx context.Context, fs *mockFileSystem) {
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

func runWalkSkipDirTest(t *testing.T, ctx context.Context, fs *mockFileSystem) {
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

func runWalkRootDotTest(t *testing.T, ctx context.Context, fs *mockFileSystem) {
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

func TestMockFile(t *testing.T) {
	t.Parallel()
	f := &mockFile{}
	n, err := f.Write([]byte("test"))
	if n != 0 || err == nil {
		t.Errorf("Write() = %d, %v; want 0, error", n, err)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestMockFileInfo(t *testing.T) {
	t.Parallel()
	info := &mockFileInfo{name: "test", size: 10, dir: true}
	if info.Name() != "test" {
		t.Errorf("Name() = %v, want test", info.Name())
	}
	if info.Size() != 10 {
		t.Errorf("Size() = %v, want 10", info.Size())
	}
	if info.Mode() != 0 {
		t.Errorf("Mode() = %v, want 0", info.Mode())
	}
	if info.ModTime().IsZero() {
		t.Error("ModTime() is zero")
	}
	if !info.IsDir() {
		t.Error("IsDir() = false, want true")
	}
	if info.Sys() != nil {
		t.Errorf("Sys() = %v, want nil", info.Sys())
	}
}

func TestMockDirEntry(t *testing.T) {
	t.Parallel()
	entry := &mockDirEntry{name: "test", isDir: true}
	if entry.Name() != "test" {
		t.Errorf("Name() = %v, want test", entry.Name())
	}
	if !entry.IsDir() {
		t.Error("IsDir() = false, want true")
	}
	if entry.Type() != 0 {
		t.Errorf("Type() = %v, want 0", entry.Type())
	}
	info, err := entry.Info()
	if info != nil || err != nil {
		t.Errorf("Info() = %v, %v; want nil, nil", info, err)
	}
}

func TestMockFileSystem_More(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := NewMockFileSystem()

	t.Run("AtomicWrite", func(t *testing.T) {
		t.Parallel()
		err := fs.AtomicWrite(ctx, "atomic.txt", []byte("data"), 0644)
		if err != nil {
			t.Fatalf("AtomicWrite failed: %v", err)
		}
		assertFileContent(t, fs, ctx, "atomic.txt", "data")
	})

	t.Run("MkdirAll", func(t *testing.T) {
		t.Parallel()
		err := fs.MkdirAll(ctx, "some/dir", 0755)
		if err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
	})

	t.Run("Open and OpenFile", func(t *testing.T) {
		t.Parallel()
		_ = fs.WriteFile(ctx, "file.txt", []byte("content"), 0644)

		f1, err := fs.Open(ctx, "file.txt")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		f1.Close()

		f2, err := fs.OpenFile(ctx, "file.txt", os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("OpenFile failed: %v", err)
		}
		f2.Close()

		_, err = fs.Open(ctx, "missing.txt")
		if err == nil {
			t.Error("Open expected error for missing file")
		}
	})

	t.Run("Stat error", func(t *testing.T) {
		t.Parallel()
		_, err := fs.Stat(ctx, "missing.txt")
		if err == nil {
			t.Error("Stat expected error for missing file")
		}
	})
}

func TestMockFileSystem_Walk_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Walk error in callback", func(t *testing.T) {
		t.Parallel()
		fs := NewMockFileSystem()
		_ = fs.WriteFile(ctx, "a.txt", []byte("a"), 0644)
		expectedErr := fmt.Errorf("walk error")
		err := fs.Walk(ctx, ".", func(path string, info os.FileInfo, err error) error {
			if path == "a.txt" {
				return expectedErr
			}
			return nil
		})
		if err != expectedErr {
			t.Errorf("Walk() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("Walk error in notifyParents", func(t *testing.T) {
		t.Parallel()
		fs := NewMockFileSystem()
		_ = fs.WriteFile(ctx, "a/b.txt", []byte("b"), 0644)
		expectedErr := fmt.Errorf("notify error")
		err := fs.Walk(ctx, ".", func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return expectedErr
			}
			return nil
		})
		if err != expectedErr {
			t.Errorf("Walk() error = %v, want %v", err, expectedErr)
		}
	})
}

func TestMockFileSystem_TableDriven(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := NewMockFileSystem()

	tests := []struct {
		name    string
		op      func() error
		wantErr bool
	}{
		{
			name: "Write and Stat",
			op: func() error {
				if err := fs.WriteFile(ctx, "test.txt", []byte("hello"), 0644); err != nil {
					return err
				}
				info, err := fs.Stat(ctx, "test.txt")
				if err != nil {
					return err
				}
				if info.Size() != 5 {
					return fmt.Errorf("expected size 5, got %d", info.Size())
				}
				return nil
			},
			wantErr: false,
		},
		{
			name: "Stat Nonexistent",
			op: func() error {
				_, err := fs.Stat(ctx, "nonexistent.txt")
				return err
			},
			wantErr: true,
		},
		{
			name: "Remove",
			op: func() error {
				_ = fs.WriteFile(ctx, "to_remove.txt", []byte("data"), 0644)
				return fs.Remove(ctx, "to_remove.txt")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.op()
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}
