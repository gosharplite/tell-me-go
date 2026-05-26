// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

func TestAtomicWrite_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	data := []byte("test-data")
	path := "/data/test.txt"

	tests := []struct {
		name       string
		setupMock  func() *mockFileSystem
		wantErr    bool
		errPattern string
	}{
		{
			name: "MkdirAll fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.MkdirAllFunc = func(ctx context.Context, path string, perm os.FileMode) error {
					return errors.New("disk full")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to create directory: disk full",
		},
		{
			name: "CreateTemp fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
					return nil, errors.New("permission denied")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to create temp file: permission denied",
		},
		{
			name: "Sync fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						SyncFunc: func() error {
							return errors.New("I/O error during sync")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to sync temp file: I/O error during sync",
		},
		{
			name: "Write fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						WriteFunc: func(p []byte) (n int, err error) {
							return 0, errors.New("disk I/O error")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to write temp file: disk I/O error",
		},
		{
			name: "Chmod fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						ChmodFunc: func(mode os.FileMode) error {
							return errors.New("chmod not supported")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to chmod temp file: chmod not supported",
		},
		{
			name: "Close fails after sync",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						CloseFunc: func() error {
							return errors.New("close failed")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to close temp file: close failed",
		},
		{
			name: "Rename fails (not EXDEV)",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.RenameFunc = func(ctx context.Context, oldpath, newpath string) error {
					return errors.New("rename denied")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to rename temp file: rename denied",
		},
		{
			name: "Rename exhausted retries",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.RenameFunc = func(ctx context.Context, oldpath, newpath string) error {
					return errors.New("Access is denied")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to rename temp file after 5 attempts: Access is denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupMock()

			err := AtomicWrite(ctx, m, path, data, 0644)

			if (err != nil) != tt.wantErr {
				t.Fatalf("AtomicWrite() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errPattern != "" {
				if err.Error() != tt.errPattern {
					t.Errorf("AtomicWrite() error = %q, want %q", err.Error(), tt.errPattern)
				}
			}
		})
	}
}

// Test A: Simulated "Disk Full" (Mock Injection)
func TestAtomicWrite_DiskFull(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()

	m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
		mf := &mockFile{
			name: dir + "/temp123",
			data: new(bytes.Buffer),
			WriteFunc: func(p []byte) (n int, err error) {
				return 0, syscall.ENOSPC
			},
		}
		return mf, nil
	}

	err := AtomicWrite(ctx, m, "/any/path", []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error for disk full (ENOSPC), got nil")
	}

	if !errors.Is(err, syscall.ENOSPC) && !strings.Contains(err.Error(), "no space left on device") {
		t.Errorf("expected ENOSPC error, got: %v", err)
	}
}

// Test B: Real OS Permission Denied (Integration Test)
func TestAtomicWrite_OSPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping OS permission test on Windows due to ACL flakiness")
	}

	tempDir := t.TempDir()
	// Create a subdirectory that we will make read-only
	targetDir := fmt.Sprintf("%s/readonly", tempDir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	// Remove write permissions from the directory
	if err := os.Chmod(targetDir, 0555); err != nil {
		t.Fatalf("failed to chmod target dir: %v", err)
	}
	defer func() { _ = os.Chmod(targetDir, 0755) }() // Clean up permissions so TempDir can be removed

	fs := &OSFileSystem{}
	err := AtomicWrite(context.Background(), fs, targetDir+"/test.txt", []byte("data"), 0644)

	if err == nil {
		t.Fatal("expected error when writing to read-only directory, got nil")
	}

	if !errors.Is(err, os.ErrPermission) && !errors.Is(err, syscall.EACCES) {
		t.Errorf("expected permission denied error, got: %v", err)
	}
}

// Test C: EXDEV Fallback Success (Mock Injection)
func TestAtomicWrite_EXDEVFallback(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()

	data := []byte("important data")
	targetPath := "/mnt/external/file.txt"

	// Configure Rename to return EXDEV
	m.RenameFunc = func(ctx context.Context, oldpath, newpath string) error {
		return syscall.EXDEV
	}

	// We need to make sure CreateTemp works and adds to files map for OpenFile to find it
	m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
		name := dir + "/temp123"
		mf := &mockFile{
			name: name,
			data: new(bytes.Buffer),
		}
		m.mu.Lock()
		m.files[name] = mf.data
		m.mu.Unlock()
		return mf, nil
	}

	err := AtomicWrite(ctx, m, targetPath, data, 0644)
	if err != nil {
		t.Fatalf("expected success with EXDEV fallback, got error: %v", err)
	}

	// Verify the data was "copied" to the target path in the mock filesystem
	m.mu.Lock()
	savedData, ok := m.files[targetPath]
	m.mu.Unlock()

	if !ok {
		t.Fatal("target file does not exist in mock filesystem after fallback")
	}

	if !bytes.Equal(savedData.Bytes(), data) {
		t.Errorf("saved data mismatch: got %q, want %q", savedData.Bytes(), data)
	}
}

type fallbackTestCase struct {
	name          string
	setupMock     func() *mockFileSystem
	wantErr       bool
	errContains   string
	expectRemoved string
}

func setupMockOpenFileSourceFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
		if name == "/src" {
			return nil, errors.New("open source failed")
		}
		return nil, os.ErrNotExist
	}
	return m
}

func setupMockOpenFileDestinationFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
		if name == "/src" {
			return &mockFile{name: name, data: new(bytes.Buffer)}, nil
		}
		if name == "/dst" {
			return nil, errors.New("open destination failed")
		}
		return nil, os.ErrNotExist
	}
	return m
}

func setupMockIoCopyFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
		data := new(bytes.Buffer)
		if name == "/src" {
			data.Write([]byte("some data"))
		}
		mf := &mockFile{
			name: name,
			data: data,
		}
		if name == "/dst" {
			mf.WriteFunc = func(p []byte) (n int, err error) {
				return 0, errors.New("copy failed")
			}
		}
		return mf, nil
	}
	return m
}

func setupMockSyncFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
		mf := &mockFile{
			name: name,
			data: new(bytes.Buffer),
		}
		if name == "/dst" {
			mf.SyncFunc = func() error {
				return errors.New("sync failed")
			}
		}
		return mf, nil
	}
	return m
}

func setupMockCloseFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(ctx context.Context, name string, flag int, perm os.FileMode) (File, error) {
		mf := &mockFile{
			name: name,
			data: new(bytes.Buffer),
		}
		if name == "/src" {
			mf.data.Write([]byte("data"))
		}
		if name == "/dst" {
			mf.CloseFunc = func() error {
				return errors.New("close failed")
			}
		}
		return mf, nil
	}
	return m
}

func buildFallbackTestCases() []fallbackTestCase {
	return []fallbackTestCase{
		{
			name:        "OpenFile source fails",
			setupMock:   setupMockOpenFileSourceFails,
			wantErr:     true,
			errContains: "fallback: failed to open source",
		},
		{
			name:        "OpenFile destination fails",
			setupMock:   setupMockOpenFileDestinationFails,
			wantErr:     true,
			errContains: "fallback: failed to open destination",
		},
		{
			name:          "io.Copy fails",
			setupMock:     setupMockIoCopyFails,
			wantErr:       true,
			errContains:   "fallback: failed to copy data",
			expectRemoved: "/dst",
		},
		{
			name:          "Sync fails",
			setupMock:     setupMockSyncFails,
			wantErr:       true,
			errContains:   "fallback: failed to sync destination",
			expectRemoved: "/dst",
		},
		{
			name:        "Close fails on success",
			setupMock:   setupMockCloseFails,
			wantErr:     true,
			errContains: "close failed",
		},
	}
}

func TestFallbackCopy_Errors(t *testing.T) {
	cases := buildFallbackTestCases()
	ctx := context.Background()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupMock()

			err := fallbackCopy(ctx, m, "/src", "/dst", 0644)

			if (err != nil) != tt.wantErr {
				t.Fatalf("fallbackCopy() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("fallbackCopy() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
			}

			if tt.expectRemoved != "" {
				found := false
				for _, r := range m.removedFiles {
					if r == tt.expectRemoved {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %s to be removed, but it wasn't. Removed: %v", tt.expectRemoved, m.removedFiles)
				}
			}
		})
	}
}

func TestCleanupOldBackups_RemoveAllError(t *testing.T) {
	var buf testfixtures.SyncWriter
	testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	m := newMockFS()
	cutoff := time.Now().AddDate(0, 0, -30)
	oldTimestamp := cutoff.AddDate(0, 0, -1).Format("20060102_150405")
	backupDirName := oldTimestamp + "_suffix"

	m.ReadDirFunc = func(ctx context.Context, name string) ([]os.DirEntry, error) {
		return []os.DirEntry{&mockDirEntry{name: backupDirName, isDir: true}}, nil
	}
	m.RemoveAllFunc = func(ctx context.Context, path string) error {
		return errors.New("remove failed")
	}

	paths := persistencePaths("home", "default")

	ctx := context.Background()
	err := cleanupOldBackups(ctx, m, *paths, 7, testLogger)
	// cleanupOldBackups never returns an error for RemoveAll failures; it only logs.
	if err != nil {
		t.Fatalf("cleanupOldBackups should not return error for RemoveAll failures: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Failed to cleanup old backup") {
		t.Errorf("expected warning 'Failed to cleanup old backup', got: %s", output)
	}
}

func TestRenameWithRetry_TransientThenPermanent(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()

	callCount := 0
	m.RenameFunc = func(ctx context.Context, oldpath, newpath string) error {
		callCount++
		if callCount == 1 {
			return errors.New("Access is denied")
		}
		return errors.New("permission denied") // non-transient
	}

	err := renameWithRetry(ctx, m, "/tmp/src", "/tmp/dst", 0644)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should exit on the second attempt (non-transient), not exhaust all 5 retries.
	if callCount != 2 {
		t.Errorf("expected 2 rename attempts, got %d", callCount)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permanent error, got: %v", err)
	}
}

func TestIsTransientRenameError_SharingViolation(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()
	// Path does not exist as a file; Stat will return an error, not IsDir.
	err := errors.New("The process cannot access the file because it is being used by another process")

	if !isTransientRenameError(ctx, m, err, "/some/file.txt") {
		t.Error("expected sharing violation to be transient")
	}
}

func TestIsTransientRenameError_DirectoryTarget(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()
	// Register the path as a directory in the mock
	m.dirs["/some/dir"] = true

	err := errors.New("Access is denied")

	if isTransientRenameError(ctx, m, err, "/some/dir") {
		t.Error("expected Access is denied on a directory to be non-transient (permanent)")
	}
}

// ---------------------------------------------------------------------------
// Context-cancellation error‑path tests for select/case <-ctx.Done() blocks
// ---------------------------------------------------------------------------

// TestAtomicWrite_CancelBeforeWrite covers line 46-47 in atomic.go:
//
//	select { case <-ctx.Done(): return ctx.Err() }  – after prepareTempFile, before Write.
//
// The mock FS does NOT check context in MkdirAll or CreateTemp, so
// prepareTempFile succeeds and the cancellation is detected at the
// first explicit select.
func TestAtomicWrite_CancelBeforeWrite(t *testing.T) {
	t.Parallel()
	m := newMockFS()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before AtomicWrite starts

	err := AtomicWrite(ctx, m, "/test", []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestAtomicWrite_CancelAfterWrite covers line 57-58 in atomic.go:
//
//	select { case <-ctx.Done(): return ctx.Err() }  – after Write, before commitTempFile.
//
// The WriteFunc blocks until the test cancels the context, guaranteeing
// that when Write returns the context is already cancelled.  Line 57's
// select then catches the cancellation.
func TestAtomicWrite_CancelAfterWrite(t *testing.T) {
	m := newMockFS()
	writeHappened := make(chan struct{})
	unblockWrite := make(chan struct{})

	m.CreateTempFunc = func(ctx context.Context, dir, pattern string) (File, error) {
		return &mockFile{
			name: dir + "/temp123",
			data: new(bytes.Buffer),
			WriteFunc: func(p []byte) (n int, err error) {
				close(writeHappened) // signal that Write was called
				<-unblockWrite       // block until cancel is done
				return len(p), nil
			},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- AtomicWrite(ctx, m, "/test", []byte("data"), 0644)
	}()

	<-writeHappened     // WriteFunc has been entered
	cancel()            // cancel before Write returns
	close(unblockWrite) // let Write return

	err := <-errCh
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestRenameWithRetry_ContextCancelledDuringBackoff covers line 114-115 in
// atomic.go:
//
//	select { case <-ctx.Done(): return ctx.Err() }  – inside the retry loop.
//
// The RenameFunc always returns a transient error ("Access is denied").
// After the first failure the function enters the backoff select; the
// test cancels the context during the sleep, causing ctx.Done() to fire.
func TestRenameWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	m := newMockFS()
	callCount := 0
	firstAttemptDone := make(chan struct{})

	m.RenameFunc = func(ctx context.Context, oldpath, newpath string) error {
		callCount++
		if callCount == 1 {
			close(firstAttemptDone) // signal first attempt completed
		}
		return errors.New("Access is denied") // always transient
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- renameWithRetry(ctx, m, "/tmp/src", "/tmp/dst", 0644)
	}()

	// Wait for first rename attempt to complete, then cancel during backoff
	<-firstAttemptDone
	cancel()

	err := <-errCh
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Should NOT exhaust all 5 retries
	if callCount >= 5 {
		t.Errorf("expected < 5 rename attempts, got %d", callCount)
	}
}

// mockDirEntry implements os.DirEntry for testing.
type mockDirEntry struct {
	name  string
	isDir bool
}

func (e *mockDirEntry) Name() string { return e.name }
func (e *mockDirEntry) IsDir() bool  { return e.isDir }
func (e *mockDirEntry) Type() os.FileMode {
	if e.isDir {
		return os.ModeDir
	}
	return 0
}
func (e *mockDirEntry) Info() (os.FileInfo, error) {
	return &mockFileInfo{name: e.name, isDir: e.isDir}, nil
}

// persistencePaths builds a Paths struct for backup cleanup testing.
func persistencePaths(homeDir, mode string) *persistence.Paths {
	return persistence.ResolvePaths(homeDir, mode)
}
