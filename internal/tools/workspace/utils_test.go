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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

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

// setupWalkTest creates a temp directory with a single file "f1.txt"
// and returns the directory path and a security manager that allows
// access to that directory.
func setupWalkTest(t *testing.T) (string, *toolstest.MockSecurityManager) {
	t.Helper()
	tempDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempDir, "safe"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "safe/f1.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.RegisterSafePath(tempDir)
	sm.IsSafeFunc = func(path string) (string, error) {
		if strings.HasPrefix(path, tempDir) {
			return path, nil
		}
		return "", os.ErrPermission
	}
	return tempDir, sm
}

func TestWalkAndProcess_SafePath(t *testing.T) {
	tempDir, sm := setupWalkTest(t)
	ctx := context.Background()
	var seen []string
	processor := func(path string) error {
		seen = append(seen, filepath.Base(path))
		return nil
	}

	err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), tempDir, nil, processor, infra_persistence.NewWorkspacePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "f1.txt" {
		t.Errorf("unexpected files seen: %v", seen)
	}
}

func TestWalkAndProcess_UnsafePath(t *testing.T) {
	_, sm := setupWalkTest(t)
	ctx := context.Background()
	err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), "/etc", nil, nil, infra_persistence.NewWorkspacePolicy())
	if err == nil {
		t.Error("expected error for unsafe path")
	}
}

func TestWalkAndProcess_EmptyPathDefaultsToDot(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(path string) (string, error) {
		if path == "." {
			return tmpDir, nil
		}
		if strings.HasPrefix(path, tmpDir) {
			return path, nil
		}
		return "", os.ErrPermission
	}

	ctx := context.Background()
	var seen []string
	processor := func(path string) error {
		seen = append(seen, filepath.Base(path))
		return nil
	}

	err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), "", nil, processor, infra_persistence.NewWorkspacePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "test.txt" {
		t.Errorf("unexpected files seen: %v", seen)
	}
}

func TestSendHeartbeat_DefaultCase(t *testing.T) {
	// Create an unbuffered channel with no reader — the send will block and fall to default
	hb := make(chan struct{})
	ctx := context.Background()

	// This should not panic; the default case prevents blocking
	sendHeartbeat(ctx, hb)

	// Verify no panic occurred (implicit — reaching here is success)
}

func TestSendHeartbeat_NilChannel(t *testing.T) {
	ctx := context.Background()
	// Nil channel — should return immediately
	sendHeartbeat(ctx, nil)
	// No panic == success
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

func TestWalkHeartbeat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		count   int
		hb      chan<- struct{}
		ctx     context.Context
		wantErr error
	}{
		{
			name:    "nil channel skips heartbeat",
			count:   50,
			hb:      nil,
			ctx:     ctx,
			wantErr: nil,
		},
		{
			name:    "non-multiple count skips heartbeat",
			count:   7,
			hb:      make(chan struct{}, 1),
			ctx:     ctx,
			wantErr: nil,
		},
		{
			name:    "heartbeat sent when conditions met",
			count:   50,
			hb:      make(chan struct{}, 1),
			ctx:     ctx,
			wantErr: nil,
		},
		{
			name:    "cancelled context error returned",
			count:   50,
			hb:      make(chan struct{}, 1),
			ctx:     cancelCtx,
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := walkHeartbeat(tt.ctx, tt.count, tt.hb)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr.Error(), err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestShouldSkipEntry(t *testing.T) {
	t.Parallel()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	policy := infra_persistence.NewWorkspacePolicy()

	regularFile := &mockFileInfo{name: "main.go", isDir: false}
	normalDir := &mockFileInfo{name: "src", isDir: true}
	dotGitDir := &mockFileInfo{name: ".git", isDir: true}

	tests := []struct {
		name     string
		ctx      context.Context
		info     os.FileInfo
		err      error
		wantSkip bool
		wantErr  error
	}{
		{
			name:     "walk error returns skip",
			ctx:      context.Background(),
			info:     nil,
			err:      errors.New("walk error"),
			wantSkip: true,
			wantErr:  nil,
		},
		{
			name:     "cancelled context returns skip with Canceled",
			ctx:      cancelCtx,
			info:     nil,
			err:      nil,
			wantSkip: true,
			wantErr:  context.Canceled,
		},
		{
			name:     "regular file not skipped",
			ctx:      context.Background(),
			info:     regularFile,
			err:      nil,
			wantSkip: false,
			wantErr:  nil,
		},
		{
			name:     "directory skipped without error",
			ctx:      context.Background(),
			info:     normalDir,
			err:      nil,
			wantSkip: true,
			wantErr:  nil,
		},
		{
			name:     "ignored directory returns SkipDir",
			ctx:      context.Background(),
			info:     dotGitDir,
			err:      nil,
			wantSkip: true,
			wantErr:  filepath.SkipDir,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSkip, gotErr := shouldSkipEntry(tt.ctx, tt.info, tt.err, policy)

			if gotSkip != tt.wantSkip {
				t.Errorf("skip = %v, want %v", gotSkip, tt.wantSkip)
			}
			if gotErr != tt.wantErr {
				t.Errorf("err = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

// isPathSafeErrorSM is a test double that embeds MockSecurityManager and
// overrides IsPathSafe to always return an error, simulating a security rejection.
type isPathSafeErrorSM struct {
	toolstest.MockSecurityManager
}

func (m *isPathSafeErrorSM) IsPathSafe(path string) (string, error) {
	return "", fmt.Errorf("path rejected")
}

func TestConcurrentSearch_IsPathSafeError(t *testing.T) {
	t.Parallel()

	sm := &isPathSafeErrorSM{}

	ctx := context.Background()
	resChan, errChan := ConcurrentSearch(ctx, sm, nil, "/tmp/test", nil, nil, infra_persistence.NewWorkspacePolicy())

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
		policy: infra_persistence.NewWorkspacePolicy(),
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
		policy: infra_persistence.NewWorkspacePolicy(),
	}

	err := p.scanFile("test.txt")
	if err == nil {
		t.Fatal("expected context.Canceled after heartbeat")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// toggleErrContext wraps a context and allows Err() to be toggled between
// nil and a target error via SetErr. This enables testing the walkHeartbeat
// error-return path in walkAndProcess (utils.go:85-87) where ctx.Err() must
// be nil during shouldSkipEntry (to avoid early abort) but non-nil during
// walkHeartbeat (to trigger the error return).
type toggleErrContext struct {
	context.Context
	mu  sync.Mutex
	err error
}

func (c *toggleErrContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return c.Context.Err()
}

func (c *toggleErrContext) setErr(err error) {
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
}

// TestWalkAndProcess_HeartbeatErrorPropagation exercises the walkHeartbeat
// error return path in walkAndProcess (utils.go:85-87). It uses a custom
// context wrapper that toggles Err() between nil (for shouldSkipEntry) and
// context.Canceled (for walkHeartbeat) so the heartbeat fires at count=50
// without the walk being aborted early by shouldSkipEntry.
func TestWalkAndProcess_HeartbeatErrorPropagation(t *testing.T) {
	t.Parallel()

	tctx := &toggleErrContext{Context: context.Background()}
	hb := make(chan struct{}, 1)

	// Create 50+ files so walkHeartbeat fires at count=50
	fs := &searchMockFS{files: make(map[string][]byte)}
	for i := 0; i < 55; i++ {
		fs.files[fmt.Sprintf("file_%d.txt", i)] = []byte("content")
	}

	sp := &mockSP{}
	policy := infra_persistence.NewWorkspacePolicy()

	processed := 0
	processor := func(path string) error {
		processed++
		// After 49 files, toggle context error so the 50th triggers
		// walkHeartbeat's error return
		if processed == 49 {
			tctx.setErr(context.Canceled)
		}
		return nil
	}

	err := walkAndProcess(tctx, sp, fs, ".", hb, processor, policy)
	if err == nil {
		t.Fatal("expected context.Canceled from walkHeartbeat")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// Should have processed exactly 49 files (50th aborted by heartbeat error)
	if processed != 49 {
		t.Errorf("expected 49 files processed before heartbeat abort, got %d", processed)
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
		policy:      infra_persistence.NewWorkspacePolicy(),
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
		policy:      infra_persistence.NewWorkspacePolicy(),
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
		policy:    infra_persistence.NewWorkspacePolicy(),
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
		policy:  infra_persistence.NewWorkspacePolicy(),
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
		policy: infra_persistence.NewWorkspacePolicy(),
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
