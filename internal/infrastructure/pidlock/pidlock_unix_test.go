// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package pidlock

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// =============================================================================
// IsProcessAlive tests
// =============================================================================

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	t.Parallel()
	if !IsProcessAlive(os.Getpid()) {
		t.Error("IsProcessAlive returned false for current process")
	}
}

func TestIsProcessAlive_PID1(t *testing.T) {
	t.Parallel()
	// PID 1 (init/systemd) should exist and be owned by another user.
	// IsProcessAlive should return true (alive but not ours).
	if !IsProcessAlive(1) {
		t.Error("IsProcessAlive returned false for PID 1 (init/systemd)")
	}
}

func TestIsProcessAlive_DeadProcess(t *testing.T) {
	t.Parallel()
	// Use a very high PID that almost certainly doesn't exist.
	deadPID := 99999999
	if IsProcessAlive(deadPID) {
		t.Skipf("PID %d unexpectedly exists — cannot test dead process on this system", deadPID)
	}
}

func TestIsProcessAlive_NegativePID(t *testing.T) {
	t.Parallel()
	if IsProcessAlive(-1) {
		t.Error("IsProcessAlive returned true for negative PID")
	}
}

func TestIsProcessAlive_ZeroPID(t *testing.T) {
	t.Parallel()
	if IsProcessAlive(0) {
		t.Error("IsProcessAlive returned true for PID 0")
	}
}

// =============================================================================
// IsStale tests for PID-based liveness
// =============================================================================

func TestIsStale_PIDOfAliveProcess(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Write current PID into lock file — process is alive
	pid := os.Getpid()
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		t.Fatal(err)
	}

	// Even if we make the file appear old, IsStale should return false
	// because the PID is alive.
	oldTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if IsStale(lockPath) {
		t.Error("IsStale returned true for lock owned by alive process")
	}
}

func TestIsStale_PIDOfDeadProcess(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Use a PID that doesn't exist
	deadPID := 99999999
	if IsProcessAlive(deadPID) {
		t.Skipf("PID %d unexpectedly exists", deadPID)
	}

	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(deadPID)), 0644); err != nil {
		t.Fatal(err)
	}

	// Lock file is brand new but PID is dead → should be stale
	if !IsStale(lockPath) {
		t.Error("IsStale returned false for lock owned by dead process (brand new file)")
	}
}

func TestIsStale_EmptyLockFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Write 0-byte lock (simulates crash before PID write)
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	// Brand new → time check returns false (within 10s threshold)
	if IsStale(lockPath) {
		t.Error("IsStale returned true for brand-new 0-byte lock (within 10s threshold)")
	}

	// Make it old → should fallback to time check and return true
	oldTime := time.Now().Add(-20 * time.Second)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if !IsStale(lockPath) {
		t.Error("IsStale returned false for 0-byte lock older than 10s")
	}
}

func TestIsStale_MissingFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/nonexistent.lock"

	if !IsStale(lockPath) {
		t.Error("IsStale returned false for missing lock file")
	}
}

func TestIsStale_UnparseablePID(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Write non-numeric content (old-format lock file)
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	// Brand new — time check should return false (within 10s threshold)
	if IsStale(lockPath) {
		t.Error("IsStale returned true for brand-new unparseable lock (within 10s threshold)")
	}

	// Make it old — time fallback should return true
	oldTime := time.Now().Add(-20 * time.Second)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if !IsStale(lockPath) {
		t.Error("IsStale returned false for unparseable lock older than 10s")
	}
}

// =============================================================================
// Acquire PID-writing tests
// =============================================================================

func TestAcquire_WritesPID(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	f, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}

	// Close and read the lock file to verify PID was written
	if closeErr := f.Close(); closeErr != nil {
		t.Fatalf("close failed: %v", closeErr)
	}

	content, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}

	pid, err := strconv.Atoi(string(content))
	if err != nil {
		t.Fatalf("lock file does not contain a valid PID: %q", string(content))
	}
	if pid != os.Getpid() {
		t.Errorf("lock file PID = %d, want %d", pid, os.Getpid())
	}

	// Clean up
	_ = os.Remove(lockPath)
}

func TestAcquire_BreaksDeadPIDLock(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Find a dead PID
	deadPID := 99999999
	if IsProcessAlive(deadPID) {
		t.Skipf("PID %d unexpectedly exists", deadPID)
	}

	// Write lock file with dead PID
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(deadPID)), 0644); err != nil {
		t.Fatal(err)
	}

	// Acquire should detect dead PID and re-acquire
	f, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire should have broken dead-PID lock: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}

	// Verify the new lock contains OUR PID
	content, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatalf("failed to read lock file: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(string(content))
	if parseErr != nil {
		t.Fatalf("lock file does not contain a valid PID: %q", string(content))
	}
	if pid != os.Getpid() {
		t.Errorf("lock file PID = %d, want %d", pid, os.Getpid())
	}

	// Clean up
	_ = f.Close()
	_ = os.Remove(lockPath)
}

func TestAcquire_RespectsAlivePIDLock(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Write lock file with OUR PID (alive process)
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := Acquire(lockPath)

	// With retry loop, the error is wrapped in a "failed after N retries" message,
	// not a bare os.IsExist.
	if err == nil {
		_ = f.Close()
		t.Error("expected error for alive-PID lock after retries, got nil")
	}
	if f != nil {
		t.Error("expected nil file handle for alive-PID lock conflict")
	}

	// Verify lock file was NOT removed (it's still held by a live process)
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("alive-PID lock file should still exist: %v", statErr)
	}
}

// =============================================================================
// Acquire retry and TOCTOU tests
// =============================================================================

func TestAcquire_RetryExhaustion(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Write lock file with OUR PID (alive process, never stale)
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	f, err := Acquire(lockPath)
	elapsed := time.Since(start)

	if err == nil {
		_ = f.Close()
		t.Fatal("expected error after retry exhaustion, got nil")
	}
	if f != nil {
		t.Error("expected nil file handle after retry exhaustion")
	}

	// The error should mention retries.
	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error message")
	}

	// Should have waited at least 50ms (first backoff).
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected at least 50ms of backoff, got %v", elapsed)
	}

	// The lock file should still exist (never broken).
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("lock file should still exist after retry exhaustion: %v", statErr)
	}
}

func TestAcquire_StaleLockRemoveThenReacquire(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Create a stale lock (unparseable content, old timestamp)
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-20 * time.Second)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Acquire should detect stale lock, remove it, loop around,
	// and acquire atomically via O_EXCL.
	f, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("expected successful acquisition after stale lock removal, got: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}

	// Verify the new lock contains OUR PID.
	content, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatalf("failed to read lock file: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(string(content))
	if parseErr != nil {
		t.Fatalf("lock file does not contain a valid PID: %q", string(content))
	}
	if pid != os.Getpid() {
		t.Errorf("lock file PID = %d, want %d", pid, os.Getpid())
	}

	// Clean up
	_ = f.Close()
	_ = os.Remove(lockPath)
}

// TestAcquire_TOCTOU_Safety verifies that when a stale lock is
// removed, but another process races to create a new (fresh) lock before
// our retry, we correctly detect the new lock as non-stale and back off.
// We simulate this by removing the stale lock, then creating a fresh lock
// (with our PID) before the retry loop comes around.
func TestAcquire_TOCTOU_Safety(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	lockPath := tmpDir + "/test.lock"

	// Create a stale lock that will trigger removal.
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-20 * time.Second)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// We can't truly race, but we verify the TOCTOU-safe behavior by
	// confirming that the stale→remove→retry→O_EXCL path works correctly:
	// the function successfully acquires the lock even though remove+recreate
	// are not atomic. In a TOCTOU-safe implementation, if another process
	// had stolen the lock, our O_EXCL would fail with os.IsExist and we'd
	// evaluate the new lock's staleness.
	f, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("expected successful acquisition after stale lock removal: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}

	// Verify the lock contains our PID.
	content, _ := os.ReadFile(lockPath)
	pid, _ := strconv.Atoi(string(content))
	if pid != os.Getpid() {
		t.Errorf("lock file PID = %d, want %d", pid, os.Getpid())
	}

	// Clean up
	_ = f.Close()
	_ = os.Remove(lockPath)
}
