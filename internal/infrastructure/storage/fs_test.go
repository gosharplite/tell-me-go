// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOSFileSystem(t *testing.T) {
	fs := &OSFileSystem{}
	ctx := context.Background()

	t.Run("WriteAndRead", func(t *testing.T) {
		testWriteAndRead(t, fs, ctx)
	})
	t.Run("StatAndMetadata", func(t *testing.T) {
		testStatAndMetadata(t, fs, ctx)
	})
	t.Run("DirectoryOps", func(t *testing.T) {
		testDirectoryOps(t, fs, ctx)
	})
	t.Run("Cleanup", func(t *testing.T) {
		testCleanup(t, fs, ctx)
	})
}

func testWriteAndRead(t *testing.T, fs FileSystem, ctx context.Context) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	data := []byte("content")

	if err := fs.WriteFile(ctx, path, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	assertFileExists(t, path)

	got, err := fs.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %s, want %s", got, data)
	}

	f, err := fs.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	f.Close()
}

func testStatAndMetadata(t *testing.T, fs FileSystem, ctx context.Context) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "meta.txt")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := fs.Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Size() != 4 {
		t.Errorf("expected size 4, got %d", info.Size())
	}
	if info.IsDir() {
		t.Error("expected not to be a directory")
	}
}

func testDirectoryOps(t *testing.T, fs FileSystem, ctx context.Context) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "a/b/c")
	if err := fs.MkdirAll(ctx, path, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	assertFileExists(t, path)

	entries, err := fs.ReadDir(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected entries")
	}

	count := 0
	err = fs.Walk(ctx, tmpDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}
	if count == 0 {
		t.Error("expected to walk at least one item")
	}
}

func testCleanup(t *testing.T, fs FileSystem, ctx context.Context) {
	tmpDir := t.TempDir()

	// Test Remove
	path := filepath.Join(tmpDir, "remove.txt")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(ctx, path); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be removed")
	}

	// Test RemoveAll
	dirPath := filepath.Join(tmpDir, "dir")
	if err := os.MkdirAll(filepath.Join(dirPath, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.RemoveAll(ctx, dirPath); err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %s to exist, but got error: %v", path, err)
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"text", []byte("hello world"), false},
		{"binary_start", []byte{0x00, 0x01, 0x02}, true},
		{"binary_middle", []byte("hello\x00world"), true},
		{"empty", []byte{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBinary(tt.data); got != tt.want {
				t.Errorf("IsBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOSFileSystem_ContextCancellation(t *testing.T) {
	fs := &OSFileSystem{}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(path, []byte("data"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("ReadDir cancelled", func(t *testing.T) {
		if _, err := fs.ReadDir(ctx, "."); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("ReadFile cancelled", func(t *testing.T) {
		if _, err := fs.ReadFile(ctx, path); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("MkdirAll cancelled", func(t *testing.T) {
		if err := fs.MkdirAll(ctx, filepath.Join(tmpDir, "new"), 0755); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("Stat cancelled", func(t *testing.T) {
		if _, err := fs.Stat(ctx, path); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("Open cancelled", func(t *testing.T) {
		if _, err := fs.Open(ctx, path); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("OpenFile cancelled", func(t *testing.T) {
		if _, err := fs.OpenFile(ctx, path, os.O_RDONLY, 0644); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("Remove cancelled", func(t *testing.T) {
		if err := fs.Remove(ctx, path); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("RemoveAll cancelled", func(t *testing.T) {
		if err := fs.RemoveAll(ctx, path); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
	t.Run("Walk cancelled", func(t *testing.T) {
		if err := fs.Walk(ctx, tmpDir, func(path string, info os.FileInfo, err error) error {
			return nil
		}); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

func TestOSFileSystem_OpenFile(t *testing.T) {
	fs := &OSFileSystem{}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "openfile.txt")
	ctx := context.Background()

	f, err := fs.OpenFile(ctx, path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist: %v", err)
	}
}
