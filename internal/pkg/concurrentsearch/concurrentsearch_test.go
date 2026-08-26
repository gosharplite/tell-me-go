// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package concurrentsearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// mockSPError is a PathValidator whose IsPathSafe always fails.
type mockSPError struct {
	mockSP
}

func (s *mockSPError) IsPathSafe(path string) (string, error) {
	return "", fmt.Errorf("path rejected")
}

// mockPolicy is a self-contained services.WorkspacePolicy for tests.
type mockPolicy struct{}

func (mockPolicy) ShouldIgnoreDir(name string) bool {
	return name == ".git" || (len(name) > 1 && name[0] == '.' && name != "..")
}
func (mockPolicy) ShouldIgnorePath(string) bool { return false }

// mockFileInfo is a directory-capable os.FileInfo for walkFunc tests.
type mockFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return 0 }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

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
			}, mockPolicy{})

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
	}, mockPolicy{})

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
	}, mockPolicy{})

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
				}, mockPolicy{})
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
		policy:      mockPolicy{},
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

func TestSearchPipeline_WalkFunc_CtxCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &searchPipeline{
		ctx:    ctx,
		policy: mockPolicy{},
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

func TestFormatMatch(t *testing.T) {
	path := "test.txt"
	lineNum := 10
	text := "  some text  "

	got := formatMatch(path, lineNum, text)
	expected := "test.txt:10: some text"
	if got != expected {
		t.Errorf("formatMatch() = %q, want %q", got, expected)
	}

	// Test truncation
	longText := strings.Repeat("a", 600)
	got = formatMatch(path, lineNum, longText)
	if !strings.Contains(got, "(truncated)") {
		t.Error("expected truncation for long line")
	}
	if len(got) > 550 { // approximate
		t.Errorf("formatted match too long: %d", len(got))
	}
}

// mockCheckBinaryFile implements persistence.File for testing checkBinary.
type mockCheckBinaryFile struct {
	readErr error
	seekErr error
	data    []byte
}

func (m *mockCheckBinaryFile) Read(p []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return copy(p, m.data), io.EOF
}

func (m *mockCheckBinaryFile) Seek(offset int64, whence int) (int64, error) {
	if m.seekErr != nil {
		return 0, m.seekErr
	}
	return 0, nil
}

func (m *mockCheckBinaryFile) Close() error                            { return nil }
func (m *mockCheckBinaryFile) Write(p []byte) (int, error)             { return 0, nil }
func (m *mockCheckBinaryFile) ReadAt(p []byte, off int64) (int, error) { return 0, nil }
func (m *mockCheckBinaryFile) ReadDir(n int) ([]os.DirEntry, error)    { return nil, nil }
func (m *mockCheckBinaryFile) Sync() error                             { return nil }

// singleFileFS is a minimal persistence.FileSystem that returns a fixed file for any Open call.
type singleFileFS struct {
	persistence.FileSystem
	file persistence.File
}

func (m *singleFileFS) Open(ctx context.Context, name string) (persistence.File, error) {
	return m.file, nil
}

func TestCheckBinary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       []byte
		readErr    error
		seekErr    error
		wantBinary bool
		wantErr    string
	}{
		{
			name:       "text file returns false",
			data:       []byte("hello world"),
			wantBinary: false,
		},
		{
			name:       "binary file returns true",
			data:       bytes.Repeat([]byte{0x00}, 512),
			wantBinary: true,
		},
		{
			name:    "read error (non-EOF) propagated",
			readErr: fmt.Errorf("disk failure"),
			wantErr: "disk failure",
		},
		{
			name:       "seek error propagated",
			data:       []byte("hello"),
			seekErr:    fmt.Errorf("seek failed"),
			wantBinary: false,
			wantErr:    "seek failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := &mockCheckBinaryFile{
				data:    tt.data,
				readErr: tt.readErr,
				seekErr: tt.seekErr,
			}

			gotBinary, gotErr := checkBinary(mock)

			if gotBinary != tt.wantBinary {
				t.Errorf("checkBinary() binary = %v, want %v", gotBinary, tt.wantBinary)
			}

			if tt.wantErr != "" {
				if gotErr == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(gotErr.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, gotErr.Error())
				}
			} else {
				if gotErr != nil {
					t.Errorf("unexpected error: %v", gotErr)
				}
			}
		})
	}
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestHeartbeatAndSendPath(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		hb           chan<- struct{}
		path         string
		pathsChanCap int
		wantErr      error
	}{
		{
			name:         "nil_hb_sends_path_successfully",
			ctx:          context.Background(),
			hb:           nil,
			path:         "test.go",
			pathsChanCap: 1,
			wantErr:      nil,
		},
		{
			name:         "non-nil_hb_sends_heartbeat_and_path",
			ctx:          context.Background(),
			hb:           make(chan struct{}, 1),
			path:         "test.go",
			pathsChanCap: 1,
			wantErr:      nil,
		},
		{
			name:         "ctx_cancelled_after_heartbeat",
			ctx:          canceledContext(),
			hb:           make(chan struct{}, 1),
			path:         "test.go",
			pathsChanCap: 1,
			wantErr:      context.Canceled,
		},
		{
			name:         "ctx_cancelled_in_select",
			ctx:          canceledContext(),
			hb:           nil,
			path:         "test.go",
			pathsChanCap: 0,
			wantErr:      context.Canceled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			p := &searchPipeline{
				pathsChan: make(chan string, tt.pathsChanCap),
				hb:        tt.hb,
			}

			// Drain pathsChan for success cases so the send doesn't block
			if tt.wantErr == nil {
				go func() { <-p.pathsChan }()
			}

			err := p.heartbeatAndSendPath(tt.ctx, tt.path)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err != tt.wantErr {
					t.Errorf("expected %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSearchPipeline_WalkFunc_ErrorSkip(t *testing.T) {
	t.Parallel()

	p := &searchPipeline{}
	err := p.walkFunc("", nil, errors.New("permission denied"))
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestScanFile_CheckBinaryReadError(t *testing.T) {
	t.Parallel()

	// scanFile calls checkBinary, which returns (false, io.ErrUnexpectedEOF).
	// scanFile then checks: if err != nil || isBin { return nil }
	// So even on read errors from checkBinary, scanFile returns nil.
	p := &searchPipeline{
		ctx:    context.Background(),
		fs:     &singleFileFS{file: &mockCheckBinaryFile{readErr: io.ErrUnexpectedEOF}},
		policy: mockPolicy{},
	}

	err := p.scanFile("corrupt.txt")
	if err != nil {
		t.Errorf("expected nil (scanFile swallows checkBinary errors), got: %v", err)
	}
}

func TestScanLines_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	content := "line1\nline2\nline3\n"
	mockFile := &mockCheckBinaryFile{
		data: []byte(content),
	}

	p := &searchPipeline{
		ctx:     ctx,
		matcher: func(path, line string) (string, bool) { return "", false },
	}

	err := p.scanLines("test.txt", mockFile)
	if err == nil {
		t.Fatal("expected context.Canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestScanFile_ContextCancellationAfterHeartbeat(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mockFile := &mockCheckBinaryFile{
		data: []byte("hello world\n"), // non-binary, passes checkBinary
	}

	p := &searchPipeline{
		ctx:    ctx,
		fs:     &singleFileFS{file: mockFile},
		hb:     make(chan struct{}, 1), // non-nil hb triggers heartbeat path
		policy: mockPolicy{},
	}

	err := p.scanFile("test.txt")
	if err == nil {
		t.Fatal("expected context.Canceled after heartbeat")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// TestStartWorkers_ScanFileContextCanceled verifies that when scanFile
// returns a context error (Canceled or DeadlineExceeded), the worker
// returns silently without sending to errChan (utils.go:222-224).
//
// GAP ACCEPTED (utils.go:215, numWorkers cap): The "numWorkers = 8"
// branch only executes on machines with >8 CPU cores. On smaller
// machines the branch is structurally unreachable. See issue #836.
func TestStartWorkers_ScanFileContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	fs := &searchMockFS{
		files: map[string][]byte{
			"test.txt": []byte("hello world\n"),
		},
	}

	p := &searchPipeline{
		ctx:         ctx,
		fs:          fs,
		pathsChan:   make(chan string, 1),
		resultsChan: make(chan string, 1),
		errChan:     make(chan error, 1),
		policy:      mockPolicy{},
		matcher:     func(_, line string) (string, bool) { return "", false },
		hb:          nil, // no heartbeat — cancellation comes from ctx
	}

	p.pathsChan <- "test.txt"
	close(p.pathsChan)

	var wg sync.WaitGroup
	p.startWorkers(&wg)
	wg.Wait()

	// errChan should be empty — worker returned silently on context error
	select {
	case err := <-p.errChan:
		t.Errorf("expected no error on errChan, got: %v", err)
	default:
		// expected: no error sent
	}
}

// TestStartWorkers_ScanFileErrorChannelFull verifies that when scanFile
// returns a non-context error and errChan is full (no receiver), the
// default case prevents blocking (utils.go:227).
func TestStartWorkers_ScanFileErrorChannelFull(t *testing.T) {
	t.Parallel()

	// Unbuffered errChan with no receiver — send will hit default
	fs := &searchMockFS{
		files: map[string][]byte{
			"badfile.txt": []byte("hello world\n"),
		},
		openReadErrs: map[string]error{
			"badfile.txt": io.ErrUnexpectedEOF,
		},
	}

	p := &searchPipeline{
		ctx:         context.Background(),
		fs:          fs,
		pathsChan:   make(chan string, 1),
		resultsChan: make(chan string, 1),
		errChan:     make(chan error), // UNBUFFERED, no receiver
		policy:      mockPolicy{},
		matcher:     func(_, line string) (string, bool) { return "", false },
	}

	p.pathsChan <- "badfile.txt"
	close(p.pathsChan)

	var wg sync.WaitGroup
	p.startWorkers(&wg)
	wg.Wait()

	// No deadlock occurred — the default case was hit.
	// The error was silently dropped (expected behavior for full channel).
}

// TestWalkFunc_LargeFileSkip verifies that walkFunc silently skips
// files larger than 1MB (utils.go walkFunc, info.Size() > 1024*1024).
func TestWalkFunc_LargeFileSkip(t *testing.T) {
	t.Parallel()

	p := &searchPipeline{
		ctx:       context.Background(),
		policy:    mockPolicy{},
		pathsChan: make(chan string, 1),
	}

	// A file > 1MB should be skipped without sending to pathsChan
	largeInfo := &searchMockFileInfo{name: "large.dat", size: 2 * 1024 * 1024}

	err := p.walkFunc("large.dat", largeInfo, nil)
	if err != nil {
		t.Errorf("expected nil (large file skipped), got: %v", err)
	}

	// Verify nothing was sent to pathsChan
	select {
	case path := <-p.pathsChan:
		t.Errorf("expected nothing on pathsChan, got: %q", path)
	default:
		// expected
	}
}

// TestScanLines_ResultsChanContextCancelled verifies that when
// ctx is cancelled while trying to send to resultsChan, scanLines
// returns ctx.Err() (utils.go:282-283).
func TestScanLines_ResultsChanContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	content := "match this line\n"
	mockFile := &mockCheckBinaryFile{data: []byte(content)}

	p := &searchPipeline{
		ctx:         ctx,
		resultsChan: make(chan string), // unbuffered — blocks send
		matcher:     func(path, line string) (string, bool) { return "", true },
	}

	// Cancel AFTER scanLines starts scanning but BEFORE the send.
	// Since resultsChan is unbuffered and there's no receiver, the
	// send blocks, then ctx.Done() fires.
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.scanLines("test.txt", mockFile)
	}()

	// Give the goroutine time to enter the select
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected context.Canceled")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scanLines to return")
	}
}

// TestScanLines_ScannerError verifies that scanLines returns
// scanner.Err() after the scan loop completes with an error
// (utils.go line 289). It routes through scanFile so that
// checkBinary consumes one Read (failAfter=1), and then
// scanLines gets the injected error on its first scanner Read.
func TestScanLines_ScannerError(t *testing.T) {
	t.Parallel()

	content := "line one\nline two\n"
	baseFile := &searchMockFile{Reader: bytes.NewReader([]byte(content)), name: "err.txt"}
	mockFile := &searchMockFileReadErr{
		searchMockFile: baseFile,
		readErr:        io.ErrUnexpectedEOF,
		failAfter:      1, // checkBinary consumes 1 read; scanLines gets error
	}

	p := &searchPipeline{
		ctx:     context.Background(),
		fs:      &singleFileFS{file: mockFile},
		policy:  mockPolicy{},
		matcher: func(path, line string) (string, bool) { return "", false },
	}

	err := p.scanFile("err.txt")
	if err == nil {
		t.Fatal("expected scanner error")
	}
	// bufio.Scanner wraps the underlying error
	if !strings.Contains(err.Error(), "UnexpectedEOF") && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("expected error related to UnexpectedEOF, got: %v", err)
	}
}

// TestWalkFunc_ShouldIgnoreDir verifies that walkFunc returns
// filepath.SkipDir for directories matching the workspace policy
// ignore list (e.g., .git). This covers utils.go:176-181
// (ShouldIgnoreDir branch within IsDir).
func TestWalkFunc_ShouldIgnoreDir(t *testing.T) {
	t.Parallel()

	p := &searchPipeline{
		ctx:    context.Background(),
		policy: mockPolicy{},
	}

	// .git directory should trigger ShouldIgnoreDir → SkipDir
	dotGitDir := &mockFileInfo{name: ".git", isDir: true}
	err := p.walkFunc(".git", dotGitDir, nil)
	if err != filepath.SkipDir {
		t.Errorf("expected filepath.SkipDir for .git, got: %v", err)
	}

	// Regular directory should not be skipped with an error
	normalDir := &mockFileInfo{name: "src", isDir: true}
	err = p.walkFunc("src", normalDir, nil)
	if err != nil {
		t.Errorf("expected nil for normal directory, got: %v", err)
	}
}

// TestScanLines_MatchNonEmpty verifies that scanLines uses the matcher's
// returned match string instead of the full line when the match is non-empty.
// This covers utils.go:3574 (if match != "").
func TestScanLines_MatchNonEmpty(t *testing.T) {
	t.Parallel()

	// Use a buffered resultsChan so the send doesn't block
	resultsChan := make(chan string, 1)

	content := "prefix: value\n"
	mockFile := &mockCheckBinaryFile{data: []byte(content)}

	p := &searchPipeline{
		ctx:         context.Background(),
		resultsChan: resultsChan,
		matcher: func(path, line string) (string, bool) {
			return "value", true // non-empty match
		},
	}

	err := p.scanLines("test.txt", mockFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case result := <-resultsChan:
		if !strings.Contains(result, "value") {
			t.Errorf("expected result containing 'value', got: %q", result)
		}
		// The result should use the match string, not the full line
		if strings.Contains(result, "prefix") {
			t.Errorf("expected match-only string, got full line: %q", result)
		}
	default:
		t.Error("expected a result on resultsChan")
	}
}

func TestConcurrentSearch_IsPathSafeError(t *testing.T) {
	t.Parallel()

	sm := &mockSPError{}

	ctx := context.Background()
	resChan, errChan := ConcurrentSearch(ctx, sm, nil, "/tmp/test", nil, nil, mockPolicy{})

	// Read the error from errChan
	err := <-errChan
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "path rejected" {
		t.Errorf("expected error %q, got %q", "path rejected", err.Error())
	}

	// Verify result channel is closed (should read zero value + ok=false)
	result, ok := <-resChan
	if ok {
		t.Errorf("expected result channel to be closed, got %q (ok=%v)", result, ok)
	}
}

// TestConcurrentSearch_IsPathSafe verifies that ConcurrentSearch returns
// an error via errChan when sp.IsPathSafe(root) fails (safety rejection).
func TestConcurrentSearch_IsPathSafe(t *testing.T) {
	t.Parallel()

	sm := &mockSPError{}

	ctx := context.Background()
	resChan, errChan := ConcurrentSearch(ctx, sm, nil, "/tmp/test", nil, nil, mockPolicy{})

	// Read the error from errChan
	err := <-errChan
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "path rejected" {
		t.Errorf("expected error %q, got %q", "path rejected", err.Error())
	}

	// Verify result channel is closed
	result, ok := <-resChan
	if ok {
		t.Errorf("expected result channel to be closed, got %q (ok=%v)", result, ok)
	}
}

// TestStartWalker_WalkError pins the startWalker top-level Walk error path
// (concurrentsearch.go:106-107): a non-context Walk failure is forwarded on
// errChan. The searchMockFS.walkErr field drives it deterministically — no
// filesystem fault injection required.
func TestStartWalker_WalkError(t *testing.T) {
	fs := &searchMockFS{
		files:    map[string][]byte{"a.txt": []byte("hello")},
		walkErr:  errors.New("walk failed"),
		openErrs: make(map[string]error),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, errChan := ConcurrentSearch(ctx, &mockSP{}, fs, ".", nil, func(_, line string) (string, bool) {
		return "", false
	}, mockPolicy{})

	select {
	case err := <-errChan:
		if err == nil || !strings.Contains(err.Error(), "walk failed") {
			t.Errorf("expected 'walk failed' error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for walk error on errChan")
	}
}
