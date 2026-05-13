// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistencetest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

// makeUnwritableDir creates a subdirectory inside t.TempDir() and removes
// write permission. On success, it returns the path to the unwritable
// directory and a path inside it for use as a target file.
//
// If running as root (euid 0), the test is skipped because root bypasses
// permission checks.
func makeUnwritableDir(t *testing.T) (dir string, fileInside string) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("skipping permission test: running as root")
	}

	dir = filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	// Remove write permission from the directory.
	if err := os.Chmod(dir, 0444); err != nil {
		t.Fatalf("failed to chmod dir: %v", err)
	}
	fileInside = filepath.Join(dir, "should-fail")
	return dir, fileInside
}

// =============================================================================
// WriteFile
// =============================================================================

func TestPlainOSFileSystem_WriteFile(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("write and read back", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "hello.txt")
		data := []byte("hello")

		err := fs.WriteFile(ctx, path, data, 0644)
		if err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		got, err := fs.ReadFile(ctx, path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(got) != string(data) {
			t.Errorf("got %q, want %q", got, data)
		}
	})

	t.Run("overwrite existing", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "overwrite.txt")

		if err := fs.WriteFile(ctx, path, []byte("old"), 0644); err != nil {
			t.Fatalf("first WriteFile failed: %v", err)
		}
		if err := fs.WriteFile(ctx, path, []byte("new"), 0644); err != nil {
			t.Fatalf("second WriteFile failed: %v", err)
		}

		got, err := fs.ReadFile(ctx, path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(got) != "new" {
			t.Errorf("got %q, want %q", got, "new")
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		t.Parallel()

		_, fileInside := makeUnwritableDir(t)

		err := fs.WriteFile(ctx, fileInside, []byte("data"), 0644)
		if err == nil {
			t.Error("expected error writing to unwritable directory, got nil")
		}
	})
}

// =============================================================================
// AtomicWrite
// =============================================================================

func TestPlainOSFileSystem_AtomicWrite(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("write and read back", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "atomic.txt")
		data := []byte("atomic-data")

		err := fs.AtomicWrite(ctx, path, data, 0644)
		if err != nil {
			t.Fatalf("AtomicWrite failed: %v", err)
		}

		got, err := fs.ReadFile(ctx, path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(got) != string(data) {
			t.Errorf("got %q, want %q", got, data)
		}
	})

	t.Run("temp file cleanup on failure", func(t *testing.T) {
		t.Parallel()

		dir, fileInside := makeUnwritableDir(t)

		// AtomicWrite should fail because the directory is unwritable.
		err := fs.AtomicWrite(ctx, fileInside, []byte("data"), 0644)
		if err == nil {
			t.Error("expected error from AtomicWrite to unwritable dir, got nil")
		}

		// Verify no atomic-* temp files were left behind.
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			t.Fatalf("ReadDir failed: %v", readErr)
		}
		for _, e := range entries {
			if matched, _ := filepath.Match("atomic-*", e.Name()); matched {
				t.Errorf("orphaned temp file found: %s", e.Name())
			}
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "atomic-overwrite.txt")

		if err := fs.AtomicWrite(ctx, path, []byte("old"), 0644); err != nil {
			t.Fatalf("first AtomicWrite failed: %v", err)
		}
		if err := fs.AtomicWrite(ctx, path, []byte("new"), 0644); err != nil {
			t.Fatalf("second AtomicWrite failed: %v", err)
		}

		got, err := fs.ReadFile(ctx, path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(got) != "new" {
			t.Errorf("got %q, want %q", got, "new")
		}
	})
}

// =============================================================================
// ReadFile
// =============================================================================

func TestPlainOSFileSystem_ReadFile(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("read existing file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "existing.txt")
		data := []byte("hello world")

		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("os.WriteFile failed: %v", err)
		}

		got, err := fs.ReadFile(ctx, path)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(got) != string(data) {
			t.Errorf("got %q, want %q", got, data)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent.txt")

		_, err := fs.ReadFile(ctx, path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("error %v does not wrap os.ErrNotExist", err)
		}
	})
}

// =============================================================================
// ReadDir
// =============================================================================

func TestPlainOSFileSystem_ReadDir(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("read directory with files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		for _, name := range []string{"a.txt", "b.txt"} {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
				t.Fatalf("os.WriteFile(%s) failed: %v", path, err)
			}
		}

		entries, err := fs.ReadDir(ctx, dir)
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("got %d entries, want 2", len(entries))
		}
	})

	t.Run("dir not found", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent-dir")

		_, err := fs.ReadDir(ctx, path)
		if err == nil {
			t.Error("expected error for nonexistent directory, got nil")
		}
	})
}

// =============================================================================
// MkdirAll
// =============================================================================

func TestPlainOSFileSystem_MkdirAll(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("create nested dirs", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		nested := filepath.Join(root, "a", "b", "c")

		if err := fs.MkdirAll(ctx, nested, 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}

		info, err := os.Stat(nested)
		if err != nil {
			t.Fatalf("os.Stat failed: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected directory, got non-directory")
		}
	})

	t.Run("already exists", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "existing-dir")

		if err := fs.MkdirAll(ctx, path, 0755); err != nil {
			t.Fatalf("first MkdirAll failed: %v", err)
		}
		if err := fs.MkdirAll(ctx, path, 0755); err != nil {
			t.Fatalf("second MkdirAll on existing dir failed: %v", err)
		}
	})
}

// =============================================================================
// Stat
// =============================================================================

func TestPlainOSFileSystem_Stat(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("stat file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "statme.txt")
		data := []byte("hello stat")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("os.WriteFile failed: %v", err)
		}

		info, err := fs.Stat(ctx, path)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if info.Size() != int64(len(data)) {
			t.Errorf("Size() = %d, want %d", info.Size(), len(data))
		}
	})

	t.Run("stat directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		info, err := fs.Stat(ctx, dir)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !info.IsDir() {
			t.Error("IsDir() = false, want true")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent")

		_, err := fs.Stat(ctx, path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("error %v does not wrap os.ErrNotExist", err)
		}
	})
}

// =============================================================================
// Open
// =============================================================================

func TestPlainOSFileSystem_Open(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("open existing file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "open-me.txt")
		data := []byte("open sesame")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("os.WriteFile failed: %v", err)
		}

		f, err := fs.Open(ctx, path)
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}

		buf := make([]byte, len(data))
		n, err := f.Read(buf)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if n != len(data) {
			t.Errorf("read %d bytes, want %d", n, len(data))
		}

		if err := f.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent")

		_, err := fs.Open(ctx, path)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

// =============================================================================
// OpenFile
// =============================================================================

func TestPlainOSFileSystem_OpenFile(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("create new file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "newfile.txt")
		data := []byte("created via OpenFile")

		f, err := fs.OpenFile(ctx, path, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			t.Fatalf("OpenFile failed: %v", err)
		}

		n, err := f.Write(data)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if n != len(data) {
			t.Errorf("wrote %d bytes, want %d", n, len(data))
		}

		if err := f.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Verify file exists with correct content.
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("os.ReadFile failed: %v", err)
		}
		if string(got) != string(data) {
			t.Errorf("got %q, want %q", got, data)
		}
	})

	t.Run("open existing", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "existing-openfile.txt")
		data := []byte("existing data")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("os.WriteFile failed: %v", err)
		}

		f, err := fs.OpenFile(ctx, path, os.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("OpenFile failed: %v", err)
		}
		defer func() { _ = f.Close() }()

		buf := make([]byte, len(data))
		n, err := f.Read(buf)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if n != len(data) {
			t.Errorf("read %d bytes, want %d", n, len(data))
		}
		if string(buf) != string(data) {
			t.Errorf("got %q, want %q", buf, data)
		}
	})
}

// =============================================================================
// Remove
// =============================================================================

func TestPlainOSFileSystem_Remove(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("remove existing file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "remove-me.txt")
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatalf("os.WriteFile failed: %v", err)
		}

		if err := fs.Remove(ctx, path); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		_, err := os.Stat(path)
		if err == nil {
			t.Error("expected file to be gone, but Stat succeeded")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected os.ErrNotExist, got %v", err)
		}
	})

	t.Run("remove nonexistent", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent-remove")

		err := fs.Remove(ctx, path)
		if err == nil {
			t.Error("expected error removing nonexistent file, got nil")
		}
	})
}

// =============================================================================
// RemoveAll
// =============================================================================

func TestPlainOSFileSystem_RemoveAll(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("remove dir tree", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		nested := filepath.Join(root, "x", "y", "z")
		if err := os.MkdirAll(nested, 0755); err != nil {
			t.Fatalf("os.MkdirAll failed: %v", err)
		}
		// Create files in the nested dirs.
		for _, p := range []string{
			filepath.Join(root, "x", "file1.txt"),
			filepath.Join(nested, "file2.txt"),
		} {
			if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
				t.Fatalf("os.WriteFile(%s) failed: %v", p, err)
			}
		}

		target := filepath.Join(root, "x")
		if err := fs.RemoveAll(ctx, target); err != nil {
			t.Fatalf("RemoveAll failed: %v", err)
		}

		_, err := os.Stat(target)
		if err == nil {
			t.Error("expected directory to be gone, but Stat succeeded")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected os.ErrNotExist, got %v", err)
		}
	})

	t.Run("remove nonexistent", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "nonexistent-removeall")

		// os.RemoveAll returns nil when the path does not exist (by design).
		err := fs.RemoveAll(ctx, path)
		if err != nil {
			t.Errorf("unexpected error removing nonexistent path: %v", err)
		}
	})
}

// =============================================================================
// Walk
// =============================================================================

func TestPlainOSFileSystem_Walk(t *testing.T) {
	t.Parallel()

	fs := persistencetest.NewPlainOSFileSystem()
	ctx := context.Background()

	t.Run("walk visits files", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		// Create a small directory tree.
		sub := filepath.Join(root, "sub")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatalf("os.MkdirAll failed: %v", err)
		}
		for _, name := range []string{"a.txt", "b.txt"} {
			path := filepath.Join(sub, name)
			if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
				t.Fatalf("os.WriteFile(%s) failed: %v", path, err)
			}
		}

		visited := make(map[string]bool)
		err := fs.Walk(ctx, root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			visited[filepath.Base(path)] = true
			return nil
		})
		if err != nil {
			t.Fatalf("Walk failed: %v", err)
		}

		// Expect root dir, sub dir, and both files.
		for _, want := range []string{filepath.Base(root), "sub", "a.txt", "b.txt"} {
			if !visited[want] {
				t.Errorf("expected %q to be visited, but it was not", want)
			}
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		sub := filepath.Join(root, "sub")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatalf("os.MkdirAll failed: %v", err)
		}
		for _, name := range []string{"a.txt", "b.txt"} {
			path := filepath.Join(sub, name)
			if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
				t.Fatalf("os.WriteFile(%s) failed: %v", path, err)
			}
		}

		ctx, cancel := context.WithCancel(ctx)
		callCount := 0

		err := fs.Walk(ctx, root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			callCount++

			// Cancel context after processing the first entry (the root directory).
			if callCount == 1 {
				cancel()
				return nil
			}

			// After cancellation, return the context error to terminate walk.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		})

		if err == nil {
			t.Fatal("expected error from cancelled context, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		// Walk should have terminated early — at most root + 1 entry processed.
		if callCount > 2 {
			t.Errorf("walk visited %d entries, want ≤ 2 (terminated early)", callCount)
		}
	})
}
