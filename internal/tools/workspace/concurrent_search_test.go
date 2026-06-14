// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type searchMockFile struct {
	*bytes.Reader
	name string
}

func (f *searchMockFile) Close() error                      { return nil }
func (f *searchMockFile) Write(p []byte) (n int, err error) { return 0, io.EOF }
func (f *searchMockFile) Sync() error                       { return nil }
func (f *searchMockFile) ReadDir(n int) ([]os.DirEntry, error) {
	return nil, io.EOF
}
func (f *searchMockFile) Seek(offset int64, whence int) (int64, error) {
	return f.Reader.Seek(offset, whence)
}

// searchMockFileReadErr wraps searchMockFile but returns an error on Read
// after a specified number of successful reads. This exercises the scanFile
// → checkBinary → scanLines → scanner.Err() error path.
type searchMockFileReadErr struct {
	*searchMockFile
	readErr   error
	readCount int
	failAfter int
}

func (f *searchMockFileReadErr) Read(p []byte) (int, error) {
	if f.readCount >= f.failAfter {
		return 0, f.readErr
	}
	f.readCount++
	return f.Reader.Read(p)
}

type searchMockFS struct {
	persistence.FileSystem
	files        map[string][]byte
	walkErr      error
	openErrs     map[string]error
	openReadErrs map[string]error // path → error returned by Read after checkBinary
}

func (m *searchMockFS) Open(ctx context.Context, name string) (persistence.File, error) {
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	f := &searchMockFile{Reader: bytes.NewReader(content), name: name}
	if readErr, ok := m.openReadErrs[name]; ok {
		return &searchMockFileReadErr{
			searchMockFile: f,
			readErr:        readErr,
			failAfter:      1, // fail after checkBinary's first read
		}, nil
	}
	return f, nil
}

func (m *searchMockFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &searchMockFileInfo{name: name, size: int64(len(content))}, nil
}

func (m *searchMockFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
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
func (s *mockSP) IsCommandAllowed(command string) bool { return true }
func (s *mockSP) IsBypassActive() bool                 { return false }

func setupSearchTest(t *testing.T) (*searchMockFS, *mockSP) {
	t.Helper()
	sp := &mockSP{}
	fs := &searchMockFS{
		files: map[string][]byte{
			"file1.txt": []byte("hello world\ntodo: fix this"),
			"file2.txt": []byte("no match here"),
			"file3.txt": []byte("another todo"),
			"bin.exe":   []byte{0, 1, 2, 3, 0},
			"large.txt": make([]byte, 2*1024*1024),
		},
		openErrs:     make(map[string]error),
		openReadErrs: make(map[string]error),
	}
	return fs, sp
}

func verifySearchResults(t *testing.T, results []string, wantCount int, wantSub []string) {
	t.Helper()
	if len(results) != wantCount {
		t.Errorf("expected %d results, got %d: %v", wantCount, len(results), results)
	}

	if len(wantSub) > 0 {
		sort.Strings(results)
		sort.Strings(wantSub)
		for i, s := range wantSub {
			if i < len(results) && results[i] != s {
				t.Errorf("result[%d] = %q, want %q", i, results[i], s)
			}
		}
	}
}

func TestConcurrentSearch(t *testing.T) {
	t.Run("TableDrivenTests", testConcurrentSearchTable)
	t.Run("Binary and Large Files are Skipped", testConcurrentSearchBinaryLarge)
	t.Run("Context Cancellation", testConcurrentSearchCancellation)
	t.Run("Race Condition Stress Test", testConcurrentSearchRace)
}

func testConcurrentSearchTable(t *testing.T) {
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
			wantCount: 1,
			wantSub:   []string{"file3.txt:1: another todo"},
			setup: func(mfs *searchMockFS) {
				mfs.openErrs["file1.txt"] = fmt.Errorf("permission denied")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, sp := setupSearchTest(t)
			if tt.setup != nil {
				tt.setup(fs)
			}

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			resChan, errChan := ConcurrentSearch(ctx, sp, fs, ".", nil, func(_, line string) (string, bool) {
				return "", strings.Contains(line, tt.query)
			}, infra_persistence.NewWorkspacePolicy())

			var results []string
			for res := range resChan {
				if len(results) >= tt.limit {
					cancel()
					break
				}
				results = append(results, res)
			}

			var finalErr error
			select {
			case err := <-errChan:
				finalErr = err
			default:
			}

			// Since error for "too many results" is handled by the caller now, we don't check for it from ConcurrentSearch
			if tt.wantErrMsg != "" && finalErr != nil {
				if finalErr.Error() != tt.wantErrMsg {
					t.Errorf("expected error %q, got %v", tt.wantErrMsg, finalErr)
				}
			}

			verifySearchResults(t, results, tt.wantCount, tt.wantSub)
		})
	}
}

func testConcurrentSearchBinaryLarge(t *testing.T) {
	fs, sp := setupSearchTest(t)
	fs.files["large.txt"] = append([]byte("todo hidden in large file"), make([]byte, 2*1024*1024)...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resChan, errChan := ConcurrentSearch(ctx, sp, fs, ".", nil, func(_, line string) (string, bool) {
		return "", strings.Contains(line, "todo")
	}, infra_persistence.NewWorkspacePolicy())

	var results []string
	for res := range resChan {
		if len(results) >= 10 {
			cancel()
			break
		}
		results = append(results, res)
	}

	var finalErr error
	select {
	case err := <-errChan:
		finalErr = err
	default:
	}

	if finalErr != nil {
		t.Fatal(finalErr)
	}
	verifySearchResults(t, results, 2, nil)
}

func testConcurrentSearchCancellation(t *testing.T) {
	fs, sp := setupSearchTest(t)
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, errChan := ConcurrentSearch(cancelCtx, sp, fs, ".", nil, func(_, line string) (string, bool) {
		return "", true
	}, infra_persistence.NewWorkspacePolicy())

	err := <-errChan
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func testConcurrentSearchRace(t *testing.T) {
	fs, sp := setupSearchTest(t)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resChan, _ := ConcurrentSearch(ctx, sp, fs, ".", nil, func(_, line string) (string, bool) {
					return "", true
				}, infra_persistence.NewWorkspacePolicy())
				for range resChan {
				}
			}()
		}
		wg.Wait()
	}
}

func TestStartWorkers_ScanFileNonContextError(t *testing.T) {
	fs := &searchMockFS{
		files: map[string][]byte{
			"badfile.txt": []byte("hello world\ntodo: fix this"),
		},
		openErrs: make(map[string]error),
		openReadErrs: map[string]error{
			"badfile.txt": io.ErrUnexpectedEOF,
		},
	}

	p := &searchPipeline{
		ctx:         context.Background(),
		fs:          fs,
		pathsChan:   make(chan string, 1),
		resultsChan: make(chan string, 1),
		errChan:     make(chan error, 1),
		policy:      infra_persistence.NewWorkspacePolicy(),
		matcher: func(_, line string) (string, bool) {
			return "", strings.Contains(line, "todo")
		},
	}

	p.pathsChan <- "badfile.txt"
	close(p.pathsChan)

	var wg sync.WaitGroup
	p.startWorkers(&wg)
	wg.Wait()

	select {
	case err := <-p.errChan:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "scanFile") {
			t.Errorf("expected error to contain 'scanFile', got: %v", err)
		}
		if !strings.Contains(err.Error(), "badfile.txt") {
			t.Errorf("expected error to contain 'badfile.txt', got: %v", err)
		}
		if !strings.Contains(err.Error(), io.ErrUnexpectedEOF.Error()) {
			t.Errorf("expected error to contain '%v', got: %v", io.ErrUnexpectedEOF, err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for error on errChan")
	}
}

func TestWalkAndProcess_HeartbeatCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	fs := &searchMockFS{
		files: make(map[string][]byte),
	}
	// Add 50+ files to trigger walkHeartbeat (count%50==0 at count=50)
	for i := 0; i < 55; i++ {
		fs.files[fmt.Sprintf("file_%d.txt", i)] = []byte("content")
	}

	sp := &mockSP{}
	hb := make(chan struct{}, 1)

	processed := 0
	err := walkAndProcess(ctx, sp, fs, ".", hb, func(path string) error {
		processed++
		return nil
	}, infra_persistence.NewWorkspacePolicy())

	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// Should have processed fewer than 55 files (aborted early)
	if processed >= 55 {
		t.Errorf("expected early abort, but processed all %d files", processed)
	}
}

func TestSearchPipeline_WalkFunc_CtxCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &searchPipeline{
		ctx:    ctx,
		policy: infra_persistence.NewWorkspacePolicy(),
	}

	// walkFunc checks p.ctx.Err() before anything else
	err := p.walkFunc("test.txt", &searchMockFileInfo{name: "test.txt", size: 10}, nil)
	if err == nil {
		t.Fatal("expected context.Canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
