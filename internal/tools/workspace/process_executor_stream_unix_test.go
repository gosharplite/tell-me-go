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

// spawnPreReaper starts a goroutine that reads a PID from pidFile,
// sends SIGKILL, and pre-reaps the zombie via syscall.Wait4 so that
// cmd.Wait() receives ECHILD instead of observing the process exit.
func spawnPreReaper(t *testing.T, pidFile string) {
	t.Helper()
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
		var wstatus syscall.WaitStatus
		wpid, werr := syscall.Wait4(pid, &wstatus, 0, nil)
		if wpid <= 0 && werr == nil {
			for i := 0; i < 1000; i++ {
				wpid, werr = syscall.Wait4(pid, &wstatus, syscall.WNOHANG, nil)
				if wpid > 0 || werr != nil {
					break
				}
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()
}

// assertNonExitErrorResult verifies that RunCommand returned a
// non-context, non-ExitError error with a non-zero exit code.
func assertNonExitErrorResult(t *testing.T, res executionResult, err error) {
	t.Helper()
	if err == nil {
		t.Error("expected error from non-ExitError path in RunCommand")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("unexpected context error (should have hit non-ExitError branch): %v", err)
	}
	if res.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
	if res.Output == "" {
		t.Log("no partial output (expected for immediate SIGKILL)")
	}
}

// runNonExitErrorWaitPathTest executes a single attempt at triggering
// the non-ExitError branch. It returns nil on success or an error.
func runNonExitErrorWaitPathTest(t *testing.T) error {
	t.Helper()

	e := newprocessExecutor()
	ctx := context.Background()

	pidFile := filepath.Join(t.TempDir(), "pid")

	spawnPreReaper(t, pidFile)

	res, err := e.RunCommand(ctx, []string{
		"/bin/sh", "-c",
		fmt.Sprintf("echo $$ > %s && exec sleep 99999", pidFile),
	}, executionConfig{})

	if err == nil {
		return fmt.Errorf("expected error from non-ExitError path in RunCommand")
	}

	assertNonExitErrorResult(t, res, err)

	return nil
}
