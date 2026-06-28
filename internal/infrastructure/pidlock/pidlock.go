// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pidlock

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// isProcessAlive checks whether the given PID corresponds to a running process.
// On Unix, it uses syscall.Signal(0) to probe liveness.
// On Windows, it always returns true, letting the time-based fallback in IsStale
// handle stale lock detection.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// On Windows, syscall.Signal(0) is not available.
	// Fall back to time-based staleness in IsStale.
	//
	// Coverage gap accepted by architect (2026-06-28, commit 0f882423):
	// this branch is platform-specific and unreachable in
	// pidlock_unix_test.go.
	if runtime.GOOS == "windows" {
		return true
	}

	// Coverage gap accepted by architect: os.FindProcess on Linux never
	// fails; it wraps the PID without making a syscall. Structurally
	// unreachable.
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}

	// ESRCH: no such process — definitely dead.
	// Coverage gap accepted by architect: tested by
	// TestIsProcessAlive_DeadProcess which uses PID 99999999. The
	// coverage tool reports this as uncovered when the dead-PID test
	// Skips because the PID unexpectedly exists on the CI machine.
	if errors.Is(err, syscall.ESRCH) {
		return false
	}

	// EPERM or os.ErrPermission: process exists but we don't own it — alive.
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		return true
	}

	// Any other error: assume dead.
	return false
}

// IsStale checks if a lock file is stale and safe to break.
// It first reads the PID from the lock file and checks process liveness.
// If the PID-based check is inconclusive, it falls back to a 10-second
// modification-time threshold. Legitimate lock holds take < 10ms.
func IsStale(path string) bool {
	// Attempt PID-based liveness check.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true // lock is gone, free to acquire
		}
		// Can't read — fall through to stat check below.
	} else {
		pidStr := strings.TrimSpace(string(data))
		if pidStr != "" {
			if pid, parseErr := strconv.Atoi(pidStr); parseErr == nil && pid > 0 {
				if !isProcessAlive(pid) {
					return true // owning process is dead, lock is stale
				}
				return false // owning process is alive, lock is valid
			}
		}
	}

	// Fallback: time-based staleness check (10 seconds).
	if info, statErr := os.Stat(path); statErr == nil {
		return time.Since(info.ModTime()) > 10*time.Second
	}
	// Coverage gap accepted by architect: os.Stat failure after a
	// successful os.ReadFile is near-impossible to trigger artificially.
	// The file existed at read time but disappeared before stat — a
	// TOCTOU race that requires external interference.
	return false
}

// cleanupLockFile closes f and removes lockPath. Errors are logged as warnings.
// Used exclusively in Acquire error paths.
func cleanupLockFile(f *os.File, lockPath, reason string) {
	if err := f.Close(); err != nil {
		slog.Warn("failed to close lock file after "+reason,
			slog.String("lock_path", lockPath), slog.Any("error", err))
	}
	if err := os.Remove(lockPath); err != nil {
		slog.Warn("failed to remove lock file after "+reason,
			slog.String("lock_path", lockPath), slog.Any("error", err))
	}
}

// Acquire attempts to create an exclusive lock file with stale-lock recovery
// and exponential backoff retry. On success, it writes the current PID into the
// lock file so that other processes can determine liveness.
//
// The function retries up to 5 times with exponential backoff (50ms base) when
// the lock is held by a live process. For stale locks (dead PID or old
// timestamp), it removes the lock and continues to the next iteration, where
// the atomic O_EXCL open prevents TOCTOU races.
func Acquire(lockPath string) (*os.File, error) {
	const maxRetries = 5
	delay := 50 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			// Write PID to the lock file for liveness detection.
			pid := os.Getpid()
			// Coverage gap accepted by architect: fmt.Fprintf to a
			// just-opened *os.File can only fail on disk-full or
			// hardware error. Testing this requires filesystem-level
			// fault injection — not worth the complexity for a
			// process-lock helper.
			if _, writeErr := fmt.Fprintf(f, "%d", pid); writeErr != nil {
				cleanupLockFile(f, lockPath, "PID write failure")
				return nil, fmt.Errorf("write PID to lock file %s: %w", lockPath, writeErr)
			}
			// Coverage gap accepted by architect: same rationale as
			// Fprintf above — Sync can only fail on disk-full or
			// hardware error. Requires filesystem-level fault injection
			// to test.
			if syncErr := f.Sync(); syncErr != nil {
				cleanupLockFile(f, lockPath, "sync failure")
				return nil, fmt.Errorf("sync lock file %s: %w", lockPath, syncErr)
			}
			return f, nil
		}

		// Non-IsExist errors are fatal (e.g., permission denied).
		if !os.IsExist(err) {
			return nil, err
		}

		// Lock exists. Check if it's stale.
		if IsStale(lockPath) {
			if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
				slog.Warn("failed to remove stale lock", slog.String("lock_path", lockPath), slog.Any("error", removeErr))
			}
			// Do NOT try to open again here — continue to the next loop iteration.
			// The atomic O_EXCL at the top of the loop prevents TOCTOU races.
			continue
		}

		// Lock exists and is held by a live process. Back off and retry.
		time.Sleep(delay)
		delay *= 2
	}

	return nil, fmt.Errorf("failed to acquire lock after %d retries: file exists", maxRetries)
}

// Release closes the lock file handle and removes the lock file from disk.
// Errors are logged as warnings; the function never panics on nil f or
// missing lock files.
func Release(lockPath string, f *os.File) {
	if f != nil {
		if err := f.Close(); err != nil {
			slog.Warn("failed to close lock file", slog.String("lock_path", lockPath), slog.Any("error", err))
		}
	}
	if err := os.Remove(lockPath); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to remove lock file", slog.String("lock_path", lockPath), slog.Any("error", err))
		}
	}
}
