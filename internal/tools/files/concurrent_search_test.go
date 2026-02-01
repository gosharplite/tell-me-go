// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

type searchMockFile struct {
	*bytes.Reader
	name string
}

func (f *searchMockFile) Close() error                                { return nil }
func (f *searchMockFile) Write(p []byte) (n int, err error)           { return 0, io.EOF }
func (f *searchMockFile) Seek(offset int64, whence int) (int64, error) { return f.Reader.Seek(offset, whence) }

type searchMockFS struct {
	fsutil.FileSystem
	files map[string][]byte
}

func (m *searchMockFS) Open(ctx context.Context, name string) (fsutil.File, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &searchMockFile{Reader: bytes.NewReader(content), name: name}, nil
}

func (m *searchMockFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &searchMockFileInfo{name: name, size: int64(len(content))}, nil
}

func (m *searchMockFS) Walk(ctx context.Context, root string, fn fsutil.WalkFunc) error {
	for path, content := range m.files {
		info := &searchMockFileInfo{name: path, size: int64(len(content))}
		if err := fn(path, info, nil); err != nil {
			if err == os.ErrNotExist {
				continue
			}
			return err
		}
	}
	return nil
}

type searchMockFileInfo struct {
	os.FileInfo
	name string
	size int64
}

func (m *searchMockFileInfo) Name() string       { return m.name }
func (m *searchMockFileInfo) Size() int64        { return m.size }
func (m *searchMockFileInfo) IsDir() bool        { return false }
func (m *searchMockFileInfo) ModTime() time.Time { return time.Now() }
func (m *searchMockFileInfo) Mode() os.FileMode  { return 0644 }
func (m *searchMockFileInfo) Sys() interface{}   { return nil }

type mockSP struct{}

func (s *mockSP) IsPathSafe(path string) (string, error) { return path, nil }
func (s *mockSP) IsPathWritable(path string) (string, error) {
	return path, nil
}
func (s *mockSP) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return true, nil
}
func (s *mockSP) TerminalLock()   {}
func (s *mockSP) TerminalUnlock() {}

func TestConcurrentSearch(t *testing.T) {
	sp := &mockSP{}
	fs := &searchMockFS{
		files: map[string][]byte{
			"file1.txt": []byte("hello world\ntodo: fix this"),
			"file2.txt": []byte("no match here"),
			"file3.txt": []byte("another todo"),
			"bin.exe":   []byte{0, 1, 2, 3, 0},
			"large.txt": make([]byte, 2*1024*1024),
		},
	}

	ctx := context.Background()

	t.Run("Basic Search", func(t *testing.T) {
		results, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
			return bytes.Contains([]byte(line), []byte("todo"))
		}, 10)
		if err != nil && err.Error() != "too many results" {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d: %v", len(results), results)
		}
	})

	t.Run("Limit Enforcement", func(t *testing.T) {
		results, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
			return true // Match everything
		}, 1)
		if err == nil || err.Error() != "too many results" {
			t.Errorf("expected 'too many results' error, got %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("Binary and Large Files are Skipped", func(t *testing.T) {
		// Fill large.txt with matchable content but it should be skipped due to size
		fs.files["large.txt"] = append([]byte("todo hidden in large file"), make([]byte, 2*1024*1024)...)

		results, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
			return bytes.Contains([]byte(line), []byte("todo"))
		}, 10)
		if err != nil && err.Error() != "too many results" {
			t.Fatal(err)
		}
		// Should still only have 2 matches from file1 and file3. bin.exe and large.txt should be skipped.
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		_, err := ConcurrentSearch(cancelCtx, sp, fs, ".", func(_, line string) bool {
			return true
		}, 10)
		if err != context.Canceled {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	})

	t.Run("Race Condition Stress Test", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			var wg sync.WaitGroup
			for j := 0; j < 5; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
						return true
					}, 5)
				}()
			}
			wg.Wait()
		}
	})
}
