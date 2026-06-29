// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package telemetry

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
)

// ---------------------------------------------------------------------------
// Unix-only logTrace / openLogFileForAppend error-path tests.
// These use syscall symbols (Mkfifo, Rlimit, Getrlimit, Setrlimit, Umask)
// that are not defined in the Windows syscall package.
// ---------------------------------------------------------------------------

func TestLogTrace_WriteError(t *testing.T) {
	// NOT parallel — uses FIFO coordination that is timing-sensitive.
	fifoPath := filepath.Join(t.TempDir(), "fifo")

	if err := syscall.Mkfifo(fifoPath, 0666); err != nil {
		t.Skipf("cannot create fifo (non-Unix?): %v", err)
	}

	trace := &domain_telemetry.TurnTrace{FinalStatus: "fifo-test"}

	// Strategy: reader opens (unblocks writer's open), then immediately closes.
	// When logTrace then calls Write, the reader is gone → EPIPE.
	readerOpened := make(chan struct{})
	readerClosed := make(chan struct{})

	go func() {
		r, err := os.OpenFile(fifoPath, os.O_RDONLY, 0)
		if err != nil {
			close(readerOpened)
			return
		}
		close(readerOpened) // signal: reader connected (both opens completed)
		_ = r.Close()       // immediately close — writer gets EPIPE on next write
		close(readerClosed)
	}()

	logDone := make(chan struct{})
	go func() {
		logTrace(context.Background(), fifoPath, trace)
		close(logDone)
	}()

	// Wait for reader to connect (unblocks logTrace's open).
	<-readerOpened
	// Wait for reader to fully close before logTrace marshals and writes.
	<-readerClosed
	// Now logTrace will marshal and write — reader is gone, so f.Write gets EPIPE.
	<-logDone
	// If we reach here, the write-error path was exercised (log.Printf warning).
}

func TestLogTrace_CloseError(t *testing.T) {
	// NOT parallel — overrides package-level var.
	//
	// Strategy: inject a mock openTraceFile that returns a file whose Write
	// method fails (simulating EFBIG/ENOSPC). This exercises the write-error
	// branch in writeTraceEntry, and subsequently the close-error branch
	// (which on real Linux/ext4 is typically unreachable — the close after a
	// failed write returns 0).
	originalOpen := openTraceFile
	t.Cleanup(func() { openTraceFile = originalOpen })

	writeErr := errors.New("simulated write error (EFBIG)")
	closeErr := errors.New("simulated close error")

	openTraceFile = func(path string) (io.WriteCloser, error) {
		return &mockFile{
			Writer:   &errorWriter{err: writeErr},
			closeErr: closeErr,
		}, nil
	}

	spy := captureSlogOutput(t)

	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "close_err.jsonl")

	trace := &domain_telemetry.TurnTrace{FinalStatus: "complete"}
	logTrace(context.Background(), traceFile, trace)

	// Verify the write-error warning was logged.
	if !spy.CalledWith("Warn", "failed to write to trace file") {
		t.Error("expected slog.Warn 'failed to write to trace file' to be logged")
	}
	// Verify the close-error warning was also logged (mock returns error on both).
	if !spy.CalledWith("Warn", "failed to close trace file") {
		t.Error("expected slog.Warn 'failed to close trace file' to be logged")
	}
}

// errorWriter is an io.Writer that always returns a predefined error.
type errorWriter struct {
	err error
}

func (w *errorWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

// TestOpenLogFileForAppend_MkdirAllSucceedsButRetryFails covers the branch
// where the first OpenFile fails with ENOENT, MkdirAll succeeds, but the
// retry OpenFile still fails. We inject a mock FileSystem that returns
// os.ErrNotExist on the first call and os.ErrPermission on the retry.
func TestOpenLogFileForAppend_MkdirAllSucceedsButRetryFails(t *testing.T) {
	// NOT parallel — overrides package-level var.
	originalFS := fileSystem
	t.Cleanup(func() { fileSystem = originalFS })

	callCount := 0
	fileSystem = &mockFS{
		openFileFunc: func(name string, flag int, perm os.FileMode) (File, error) {
			callCount++
			if callCount == 1 {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrPermission
		},
		mkdirAllFunc: func(path string, perm os.FileMode) error {
			return nil // MkdirAll succeeds
		},
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "missing", "log.jsonl")

	_, err := openLogFileForAppend(logPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the error is from the retry (after mkdir), not from mkdir itself.
	errStr := err.Error()
	if !strings.Contains(errStr, "after mkdir") {
		t.Errorf("error should mention 'after mkdir', got: %v", err)
	}
}
