// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package fsutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOSFileSystem(t *testing.T) {
	fs := &OSFileSystem{}
	tmpDir := t.TempDir()
	ctx := context.Background()

	t.Run("Write and Read File", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test.txt")
		data := []byte("content")
		if err := fs.WriteFile(ctx, path, data, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		got, err := fs.ReadFile(ctx, path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(got) != string(data) {
			t.Errorf("got %s, want %s", got, data)
		}
	})

	t.Run("ReadDir", func(t *testing.T) {
		entries, err := fs.ReadDir(ctx, tmpDir)
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) == 0 {
			t.Error("expected at least one entry")
		}
	})

	t.Run("MkdirAll and Stat", func(t *testing.T) {
		path := filepath.Join(tmpDir, "a/b/c")
		if err := fs.MkdirAll(ctx, path, 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
		info, err := fs.Stat(ctx, path)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected directory")
		}
	})

	t.Run("Open and Close", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test.txt")
		f, err := fs.Open(ctx, path)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		f.Close()
	})

	t.Run("Remove", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test.txt")
		if err := fs.Remove(ctx, path); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
		_, err := fs.Stat(ctx, path)
		if !os.IsNotExist(err) {
			t.Error("expected file to be removed")
		}
	})

	t.Run("Walk", func(t *testing.T) {
		count := 0
		err := fs.Walk(ctx, tmpDir, func(path string, info os.FileInfo, err error) error {
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
			t.Error("expected to walk at least one file/dir")
		}
	})
}
