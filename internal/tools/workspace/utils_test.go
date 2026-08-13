// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

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

// ---------------------------------------------------------------------------
// countingErrContext — context wrapper that returns nil the first N times
// Err() is called, then switches to returning a target error. This enables
// testing the walkHeartbeat error return in walkAndProcess (utils.go:85-87)
// which is normally unreachable because shouldSkipEntry (called immediately
// before walkHeartbeat) also checks ctx.Err().
// ---------------------------------------------------------------------------

type countingErrContext struct {
	context.Context
	mu        sync.Mutex
	callCount int
	failAfter int
	targetErr error
}

func (c *countingErrContext) Err() error {
	c.mu.Lock()
	c.callCount++
	count := c.callCount
	c.mu.Unlock()
	if count > c.failAfter {
		return c.targetErr
	}
	return c.Context.Err()
}

// TestWalkAndProcess_IsPathSafe covers two error paths in walkAndProcess:
//  1. sm.IsPathSafe(path) returning an error (safety rejection)
//  2. walkHeartbeat returning a context error (utils.go:85-87)
//
// Path (2) uses countingErrContext so that shouldSkipEntry sees nil from
// ctx.Err() (allowing the file to proceed) while walkHeartbeat sees
// context.Canceled (triggering the error return).
func TestWalkAndProcess_IsPathSafe(t *testing.T) {
	t.Parallel()

	t.Run("IsPathSafe error", func(t *testing.T) {
		t.Parallel()
		_, sm := setupWalkTest(t)
		ctx := context.Background()
		err := walkAndProcess(ctx, sm, persistencetest.NewPlainOSFileSystem(), "/etc", nil, nil, infra_persistence.NewWorkspacePolicy())
		if err == nil {
			t.Error("expected error for unsafe path")
		}
	})

	t.Run("walkHeartbeat context error", func(t *testing.T) {
		t.Parallel()

		// Use a context that cancels on a toggle: after the processor function
		// has been called 49 times (meaning 49 files have passed through
		// shouldSkipEntry and walkHeartbeat), the NEXT file (50th) will have
		// shouldSkipEntry see nil (toggle not yet active) and walkHeartbeat see
		// Canceled (toggle activated inside shouldSkipEntry's ctx.Err() check).
		//
		// We use a deterministic FS that walks files in order, and toggle the
		// context error INSIDE the counting logic so that for file #50 the
		// first ctx.Err() call (shouldSkipEntry) returns nil but the second
		// (walkHeartbeat) returns Canceled.
		tctx := &countingErrContext{
			Context:   context.Background(),
			failAfter: 50, // file 50: shouldSkipEntry=call#50→nil, walkHeartbeat=call#51→Canceled
			targetErr: context.Canceled,
		}

		hb := make(chan struct{}, 1)

		// Build a deterministic file list (slice, not map) so files are
		// processed in a guaranteed order.
		files := make([]string, 55)
		for i := 0; i < 55; i++ {
			files[i] = fmt.Sprintf("file_%04d.txt", i)
		}

		fs := &orderedWalkFS{
			files: files,
			sizes: make([]int64, 55),
		}
		for i := range fs.sizes {
			fs.sizes[i] = int64(len("content"))
		}

		sp := &mockSP{}
		policy := infra_persistence.NewWorkspacePolicy()

		err := walkAndProcess(tctx, sp, fs, ".", hb, func(path string) error {
			return nil
		}, policy)

		if err == nil {
			t.Fatal("expected context.Canceled from walkHeartbeat")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	})
}

// orderedWalkFS is a minimal persistence.FileSystem that walks files in
// a deterministic order (by slice index). Used when test assertions depend
// on a guaranteed walk ordering (e.g., testing the walkHeartbeat error
// return at exactly the 50th file).
type orderedWalkFS struct {
	persistence.FileSystem
	files []string
	sizes []int64
}

func (m *orderedWalkFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	for i, path := range m.files {
		info := &searchMockFileInfo{name: path, size: m.sizes[i]}
		if err := fn(path, info, nil); err != nil {
			if err == os.ErrNotExist {
				continue
			}
			return err
		}
	}
	return nil
}

func (m *orderedWalkFS) Open(ctx context.Context, name string) (persistence.File, error) {
	return &mockCheckBinaryFile{data: []byte("content")}, nil
}
