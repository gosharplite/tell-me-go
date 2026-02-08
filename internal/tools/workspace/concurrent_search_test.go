// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

type searchMockFile struct {
	*bytes.Reader
	name string
}

func (f *searchMockFile) Close() error                      { return nil }
func (f *searchMockFile) Write(p []byte) (n int, err error) { return 0, io.EOF }
func (f *searchMockFile) Seek(offset int64, whence int) (int64, error) {
	return f.Reader.Seek(offset, whence)
}

type searchMockFS struct {
	fsutil.FileSystem
	files    map[string][]byte
	walkErr  error
	openErrs map[string]error
}

func (m *searchMockFS) Open(ctx context.Context, name string) (fsutil.File, error) {
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}
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
	if m.walkErr != nil {
		return m.walkErr
	}
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
func (s *mockSP) TerminalLock()                        {}
func (s *mockSP) TerminalUnlock()                      {}
func (s *mockSP) IsCommandAllowed(command string) bool { return true }

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
		openErrs: make(map[string]error),
	}

	ctx := context.Background()

	tests := []struct {
		name       string
		query      string
		limit      int
		wantCount  int
		wantSub    []string
		wantErrMsg string
		setup      func(*searchMockFS)
	}{
		{
			name:      "Basic Search",
			query:     "todo",
			limit:     10,
			wantCount: 2,
			wantSub:   []string{"file1.txt:2: todo: fix this", "file3.txt:1: another todo"},
		},
		{
			name:       "Limit Enforcement",
			query:      "todo",
			limit:      1,
			wantCount:  1,
			wantErrMsg: "too many results",
		},
		{
			name:      "No matches",
			query:     "nonexistent",
			limit:     10,
			wantCount: 0,
		},
		{
			name:      "File Open Error Handling",
			query:     "todo",
			limit:     10,
			wantCount: 1, // file1.txt:2 fails open, only file3.txt:1 matches
			wantSub:   []string{"file3.txt:1: another todo"},
			setup: func(mfs *searchMockFS) {
				mfs.openErrs["file1.txt"] = fmt.Errorf("permission denied")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset or setup FS
			fs.openErrs = make(map[string]error)
			if tt.setup != nil {
				tt.setup(fs)
			}

			results, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
				return bytes.Contains([]byte(line), []byte(tt.query))
			}, tt.limit)

			if tt.wantErrMsg != "" {
				if err == nil || err.Error() != tt.wantErrMsg {
					t.Errorf("expected error %q, got %v", tt.wantErrMsg, err)
				}
			} else if err != nil && err.Error() != "too many results" {
				t.Fatal(err)
			}

			if len(results) != tt.wantCount {
				t.Errorf("expected %d results, got %d: %v", tt.wantCount, len(results), results)
			}

			if len(tt.wantSub) > 0 {
				sort.Strings(results)
				sort.Strings(tt.wantSub)
				for i, s := range tt.wantSub {
					if i < len(results) && results[i] != s {
						t.Errorf("result[%d] = %q, want %q", i, results[i], s)
					}
				}
			}
		})
	}

	t.Run("Binary and Large Files are Skipped", func(t *testing.T) {
		fs.openErrs = make(map[string]error)
		fs.files["large.txt"] = append([]byte("todo hidden in large file"), make([]byte, 2*1024*1024)...)

		results, err := ConcurrentSearch(ctx, sp, fs, ".", func(_, line string) bool {
			return bytes.Contains([]byte(line), []byte("todo"))
		}, 10)
		if err != nil && err.Error() != "too many results" {
			t.Fatal(err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

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
