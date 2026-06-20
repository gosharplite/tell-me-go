// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// ---------------------------------------------------------------------------
// Gap 8 — ReadPage error wrapping for non-ErrNotExist errors
// Code path: archive_reader.go L59-67
//   "read page from %s at %d: %w"
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_ReadPage_NonErrNotExistError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	baseFS := persistence.NewOSFileSystem()
	// Seed the archive on the real filesystem so Open succeeds
	if err := baseFS.WriteFile(context.Background(), archivePath,
		[]byte(`{"role":"user","parts":[{"text":"hello"}]}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to seed archive: %v", err)
	}

	// readAtErr causes ReadAt in readPageInternal → SectionReader → bufio to fail
	mfs := &mockFS{FileSystem: baseFS, readAtErr: errors.New("injected readat error")}

	reader := &jsonlArchiveReader{
		fs:          mfs,
		archivePath: archivePath,
	}

	_, _, err := reader.ReadPage(context.Background(), 10, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the error wrapping format
	if !strings.Contains(err.Error(), "read page from") {
		t.Errorf("expected error containing %q, got %q", "read page from", err.Error())
	}
	if !strings.Contains(err.Error(), archivePath) {
		t.Errorf("expected error to contain archive path %q, got %q", archivePath, err.Error())
	}

	// Verify it is NOT an ErrNotExist (which would be silently swallowed)
	if errors.Is(err, os.ErrNotExist) {
		t.Error("expected non-ErrNotExist error, got ErrNotExist")
	}
}

// ---------------------------------------------------------------------------
// Gap 9 — ensureIndex fast path after OnceWithRetry success
// Code path: archive_reader.go ensureIndex → indexOnce.Do → done.Load()
//   First call primes OnceWithRetry, second call hits lock-free fast path
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_EnsureIndex_AlreadyIndexed(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	baseFS := persistence.NewOSFileSystem()
	// Seed with valid JSONL so ensureIndex succeeds
	if err := baseFS.WriteFile(context.Background(), archivePath,
		[]byte(`{"role":"user","parts":[{"text":"hello"}]}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to seed archive: %v", err)
	}

	reader := &jsonlArchiveReader{
		fs:          baseFS,
		archivePath: archivePath,
	}

	// Prime the OnceWithRetry: first call builds the index
	err := reader.ensureIndex(context.Background())
	if err != nil {
		t.Fatalf("expected nil error on first ensureIndex, got %v", err)
	}

	// Second call hits the fast path (lock-free atomic check)
	err = reader.ensureIndex(context.Background())
	if err != nil {
		t.Errorf("expected nil error when already indexed (fast path), got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Gap 10 — ReadPrevious error propagation when readPageInternal fails
// Code path: archive_reader.go L90-92
//   dtos, _, err := r.readPageInternal(...)
//   if err != nil { return nil, 0, err }
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_ReadPrevious_ReadPageInternalError(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	baseFS := persistence.NewOSFileSystem()
	// Seed with 2 valid JSON lines so ensureIndex builds a non-empty index
	if err := baseFS.WriteFile(context.Background(), archivePath,
		[]byte(
			`{"role":"user","parts":[{"text":"line1"}]}`+"\n"+
				`{"role":"model","parts":[{"text":"line2"}]}`+"\n",
		), 0644); err != nil {
		t.Fatalf("failed to seed archive: %v", err)
	}

	// readAtErr causes ReadAt in readPageInternal → SectionReader → bufio to fail
	mfs := &mockFS{FileSystem: baseFS, readAtErr: errors.New("injected readat error")}

	reader := &jsonlArchiveReader{
		fs:          mfs,
		archivePath: archivePath,
	}

	// ReadPrevious(ctx, 10, -1):
	//   1. ensureIndex succeeds (Open on real FS works)
	//   2. startOffset computed from index
	//   3. readPageInternal fails because mockFS.Open returns file with readAtErr
	_, _, err := reader.ReadPrevious(context.Background(), 10, -1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "injected readat error") {
		t.Errorf("expected error containing %q, got %q", "injected readat error", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Gap 11 — ctx.Done() cancellation in readPageInternal loop
// Code path: archive_reader.go L178-179
//   select { case <-ctx.Done(): return nil, 0, ctx.Err() ... }
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_ReadPageInternal_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	baseFS := persistence.NewOSFileSystem()
	// Seed with multiple lines so the loop runs and hits ctx.Done()
	if err := baseFS.WriteFile(context.Background(), archivePath,
		[]byte(
			`{"role":"user","parts":[{"text":"line1"}]}`+"\n"+
				`{"role":"user","parts":[{"text":"line2"}]}`+"\n"+
				`{"role":"user","parts":[{"text":"line3"}]}`+"\n",
		), 0644); err != nil {
		t.Fatalf("failed to seed archive: %v", err)
	}

	reader := &jsonlArchiveReader{
		fs:          baseFS,
		archivePath: archivePath,
	}

	tests := []struct {
		name    string
		ctx     context.Context
		wantErr error
	}{
		{
			name: "canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
				cancel()
				return ctx
			}(),
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := reader.readPageInternal(tt.ctx, 10, 0)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Gap 3 (Issue #949) — OnceWithRetry concurrent deduplication
// Code path: concurrency/once.go Do() → mu.Lock() + double-check done.Load()
//   Two goroutines enter Do() simultaneously; Mutex serializes them;
//   second goroutine observes done==true after first completes
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_EnsureIndex_DoubleCheckLocked(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	baseFS := persistence.NewOSFileSystem()
	// Seed with enough lines to make index building take non-zero time
	var content string
	for i := 0; i < 1000; i++ {
		content += `{"role":"user","parts":[{"text":"msg"}]}` + "\n"
	}
	if err := baseFS.WriteFile(context.Background(), archivePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to seed archive: %v", err)
	}

	reader := &jsonlArchiveReader{
		fs:          baseFS,
		archivePath: archivePath,
	}

	// Barrier to ensure both goroutines enter ensureIndex at roughly the same time
	var start sync.WaitGroup
	start.Add(2)

	var wg sync.WaitGroup
	wg.Add(2)

	errs := make(chan error, 2)

	go func() {
		defer wg.Done()
		start.Done()
		start.Wait()
		errs <- reader.ensureIndex(context.Background())
	}()

	go func() {
		defer wg.Done()
		start.Done()
		start.Wait()
		errs <- reader.ensureIndex(context.Background())
	}()

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("ensureIndex should not error: %v", err)
		}
	}

	// Both calls should succeed and the index should be built
	if len(reader.index) == 0 {
		t.Error("expected reader to have a non-empty index after concurrent ensureIndex calls")
	}
	if len(reader.index) != 1000 { // 1000 lines
		t.Errorf("expected 1000 index entries, got %d", len(reader.index))
	}
}

// ---------------------------------------------------------------------------
// readLines L181-183 — context cancellation during line iteration
// Code path: archive_reader.go L181-183
//   if err := r.checkContext(ctx); err != nil { return nil, 0, err }
//
// We bypass readPageInternal and call readLines directly with a live *os.File
// opened via os.Open (not through the FS) so fsRetry doesn't pre-empt the
// cancellation before the loop begins.
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_ReadLines_ContextCancelledMidIteration(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "archive.jsonl")

	baseFS := persistence.NewOSFileSystem()

	// Write 3+ lines so the loop enters and hits checkContext
	if err := baseFS.WriteFile(context.Background(), archivePath,
		[]byte(
			`{"role":"user","parts":[{"text":"line1"}]}`+"\n"+
				`{"role":"model","parts":[{"text":"line2"}]}`+"\n"+
				`{"role":"user","parts":[{"text":"line3"}]}`+"\n",
		), 0644); err != nil {
		t.Fatalf("failed to write archive: %v", err)
	}

	reader := &jsonlArchiveReader{
		fs:          baseFS,
		archivePath: archivePath,
	}

	tests := []struct {
		name    string
		ctx     context.Context
		wantErr error
	}{
		{
			name: "canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
				cancel()
				return ctx
			}(),
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := os.Open(archivePath)
			if err != nil {
				t.Fatalf("failed to open archive: %v", err)
			}
			defer func() { _ = file.Close() }()

			_, _, err = reader.readLines(tt.ctx, 10, 0, file)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkContext L203-204 — ctx.Done() signal handling
// Code path: archive_reader.go L203-204
//   case <-ctx.Done(): return ctx.Err()
// ---------------------------------------------------------------------------

func TestJSONLArchiveReader_CheckContext_Done(t *testing.T) {
	reader := &jsonlArchiveReader{}

	tests := []struct {
		name    string
		ctx     context.Context
		wantErr error
	}{
		{
			name: "canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
				cancel()
				return ctx
			}(),
			wantErr: context.DeadlineExceeded,
		},
		{
			name:    "not cancelled",
			ctx:     context.Background(),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reader.checkContext(tt.ctx)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}
