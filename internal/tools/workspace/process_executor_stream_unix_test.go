// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestRunCommand_NonExitErrorWaitPath_SIGKILL directly exercises the
// non-*exec.ExitError wait branch in RunCommand (lines 82-84).
// Using context.Background() bypasses the context check at lines 77-79.
//
// Approach: a shell writes its PID to a temp file, then execs a long
// sleep. A background goroutine kills the PID and pre-reaps the zombie
// via syscall.Wait4 before cmd.Wait() runs, forcing ECHILD ("no child
// processes") — which is *os.SyscallError, not *exec.ExitError.
// An internal retry loop absorbs the inherent goroutine scheduling
// race between the pre-reap and cmd.Wait().
func TestRunCommand_NonExitErrorWaitPath_SIGKILL(t *testing.T) {
	const maxAttempts = 5
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		lastErr = runNonExitErrorWaitPathTest(t)
		if lastErr == nil {
			return // success
		}
	}
	t.Fatal(lastErr)
}

// runNonExitErrorWaitPathTest executes a single attempt at triggering
// the non-ExitError branch. It returns nil on success or an error.
func runNonExitErrorWaitPathTest(t *testing.T) error {
	t.Helper()

	e := newprocessExecutor()
	ctx := context.Background() // NOT a cancellable context

	pidFile := filepath.Join(t.TempDir(), "pid")

	// Background goroutine: kill the sleep process and pre-reap it
	// before cmd.Wait() is called internally by RunCommand. Using
	// syscall.Wait4 in a tight loop reliably beats Go's SIGCHLD
	// handler, forcing cmd.Wait() to receive ECHILD.
	go func() {
		var pid int
		for i := 0; i < 50; i++ {
			data, err := os.ReadFile(pidFile)
			if err == nil {
				_, _ = fmt.Sscanf(string(data), "%d", &pid)
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if pid <= 0 {
			return
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		// Immediate blocking reap: after SIGKILL the kernel
		// makes the process a zombie immediately. A blocking
		// Wait4 reliably reaps it before Go's internal SIGCHLD
		// handler or cmd.Wait() does.
		var wstatus syscall.WaitStatus
		wpid, werr := syscall.Wait4(pid, &wstatus, 0, nil)
		if wpid <= 0 && werr == nil {
			// Fallback: WNOHANG polling loop (should be
			// rarely reached; only if the kernel hasn't
			// delivered the zombie yet).
			for i := 0; i < 1000; i++ {
				wpid, werr = syscall.Wait4(pid, &wstatus, syscall.WNOHANG, nil)
				if wpid > 0 || werr != nil {
					break
				}
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	res, err := e.RunCommand(ctx, []string{
		"/bin/sh", "-c",
		fmt.Sprintf("echo $$ > %s && exec sleep 99999", pidFile),
	}, executionConfig{})

	if err == nil {
		return fmt.Errorf("expected error from non-ExitError path in RunCommand")
	}

	// Verify it's not a context error (we used context.Background())
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("unexpected context error (should have hit non-ExitError branch): %v", err)
	}

	if res.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}

	if res.Output == "" {
		t.Log("no partial output (expected for immediate SIGKILL)")
	}

	return nil
}
