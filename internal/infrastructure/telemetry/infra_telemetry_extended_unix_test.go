// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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
	// NOT parallel — uses RLIMIT_FSIZE to trigger EFBIG on write, and
	// attempts to also trigger a close(2) error.
	//
	// Strategy:
	//  1. Set RLIMIT_FSIZE to a small value (1KB).
	//  2. Create a large TurnTrace whose JSON exceeds the limit.
	//  3. logTrace opens (succeeds), writes → EFBIG (write-error path).
	//  4. close(2) after a failed O_APPEND write: on Linux/ext4 this
	//     typically returns 0, so the close-error body is unreachable
	//     in practice. We retain this test to prove the path exists and
	//     document the limitation.

	tempDir := t.TempDir()
	traceFile := filepath.Join(tempDir, "close_err.jsonl")

	// Build a trace whose JSON exceeds ~1KB.
	execs := make([]domain_telemetry.ToolExecutionTrace, 200)
	for i := range execs {
		execs[i] = domain_telemetry.ToolExecutionTrace{
			ToolName:  "very-long-tool-name-to-fill-bytes",
			StartTime: time.Now(),
			Duration:  time.Second,
			Status:    "success",
			Error:     "a detailed error message that takes up space in the JSON output",
		}
	}
	trace := &domain_telemetry.TurnTrace{
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(time.Minute),
		InferenceDuration: 30 * time.Second,
		ToolExecutions:    execs,
		FinalStatus:       "complete",
	}

	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
		t.Skipf("cannot get rlimit: %v", err)
	}
	defer func() { _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig) }()

	lim := orig
	lim.Cur = 1024 // 1KB — trace JSON is ~30KB
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Skipf("cannot set rlimit: %v", err)
	}

	// logTrace will open, marshal, write → EFBIG, then close.
	// Write-error path is covered. Close-error path depends on kernel/fs.
	logTrace(context.Background(), traceFile, trace)

	// Verify the file was created (it should exist but be truncated).
	if fi, err := os.Stat(traceFile); err == nil {
		t.Logf("file size after EFBIG write: %d bytes", fi.Size())
	}
}

// TestOpenLogFileForAppend_MkdirAllSucceedsButRetryFails covers the branch
// where the first OpenFile fails with ENOENT, MkdirAll succeeds, but the
// retry OpenFile still fails. We use syscall.Umask(0o777) so that MkdirAll
// creates the parent directory with mode 0000, causing the retry to fail
// with EACCES.
//
// NOT parallel — syscall.Umask is process-global.
func TestOpenLogFileForAppend_MkdirAllSucceedsButRetryFails(t *testing.T) {
	// NOT parallel: umask is process-global.
	tmpDir := t.TempDir()

	// Only ONE level of missing directory so MkdirAll itself succeeds.
	logPath := filepath.Join(tmpDir, "missing", "log.jsonl")

	// Set umask so MkdirAll creates the parent dir with mode 0000.
	oldUmask := syscall.Umask(0o777)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	_, err := openLogFileForAppend(logPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the error is from the retry (after mkdir), not from mkdir itself.
	errStr := err.Error()
	if !strings.Contains(errStr, "after mkdir") {
		t.Errorf("error should mention 'after mkdir', got: %v", err)
	}
	// Verify the directory WAS created (MkdirAll succeeded).
	if _, statErr := os.Stat(filepath.Dir(logPath)); os.IsNotExist(statErr) {
		t.Error("parent directory was not created by MkdirAll")
	}
}
