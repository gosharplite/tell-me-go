// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package pidlock

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// =============================================================================
// IsStale tests
// =============================================================================

func TestIsStale(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "is_stale_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// 1. Create a fresh lock
	err = os.WriteFile(lockPath, []byte("lock"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 2. IsStale should be false if it's new
	if IsStale(lockPath) {
		t.Error("IsStale returned true for a fresh lock")
	}

	// 3. Make it old
	oldTime := time.Now().Add(-10 * time.Minute)
	err = os.Chtimes(lockPath, oldTime, oldTime)
	if err != nil {
		t.Fatal(err)
	}

	// 4. IsStale should now be true
	if !IsStale(lockPath) {
		t.Error("IsStale returned false for a stale lock")
	}
}

// =============================================================================
// Release error path tests
// =============================================================================

func TestRelease_CloseError(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "release_lock_close_error_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	historyPath := filepath.Join(tmpDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Create a real lock file
	if err := os.WriteFile(lockPath, []byte("lock"), 0644); err != nil {
		t.Fatal(err)
	}

	// Open the lock file to get an *os.File handle
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Close the underlying file descriptor BEFORE calling Release.
	// The subsequent f.Close() inside Release will return an error
	// (double-close / use of closed file).
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Release must not panic when Close returns an error,
	// and os.Remove must still execute (lock file cleaned up).
	Release(lockPath, f)

	// Verify lock file was removed despite Close error
	if _, err := os.Stat(lockPath); err == nil {
		t.Error("lock file should have been removed even when Close fails")
	}
}

func TestRelease_RemoveError(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "release_lock_remove_error_test")
	if err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a real lock file
	if err := os.WriteFile(lockPath, []byte("lock"), 0644); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatal(err)
	}

	// Open the lock file normally — Close will succeed
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0644)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	// Make the directory read-only so os.Remove fails with a permission error.
	// Skip on Windows where Chmod doesn't work the same way.
	if err := os.Chmod(tmpDir, 0555); err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Skipf("cannot chmod tmp dir (likely Windows): %v", err)
	}

	// Restore permissions for cleanup regardless of outcome
	defer func() {
		_ = os.Chmod(tmpDir, 0755)
		_ = os.RemoveAll(tmpDir)
	}()

	// Release must not panic when os.Remove fails with a
	// non-NotExist error (permission denied).
	Release(lockPath, f)

	// If we reach here without panic, the error path was handled correctly.
}

func TestRelease_NilFile(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "release_lock_nil_file_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Release must not panic when f is nil.
	// The lock file may or may not exist — the test only asserts no panic.
	Release(lockPath, nil)

	// Additionally verify it doesn't panic when the lock file also doesn't exist
	Release(lockPath, nil)
}

// =============================================================================
// Acquire tests
// =============================================================================

func TestAcquire_StaleLock_RemoveSucceeds(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "acquire_stale_ok_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a stale lock file
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Acquire should break the stale lock and re-acquire
	f, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}

	// Verify the lock file now exists (was re-created)
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("lock file should exist after re-acquire: %v", statErr)
	}

	// Clean up
	Release(lockPath, f)

	// Verify lock was cleaned up
	if _, statErr := os.Stat(lockPath); statErr == nil {
		t.Error("lock file should be removed after release")
	}
}

func TestAcquire_FreshLock_NotStale(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "acquire_fresh_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a fresh lock file (just created, not stale)
	if err := os.WriteFile(lockPath, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := Acquire(lockPath)

	// With retry loop, the error is "failed after N retries", not os.IsExist.
	if err == nil {
		t.Error("expected error for fresh lock after retries, got nil")
	}
	if f != nil {
		t.Error("expected nil file handle for fresh lock conflict")
	}

	// Verify the lock file was NOT removed (it's still fresh/valid)
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("fresh lock file should still exist: %v", statErr)
	}
}

func TestAcquire_StaleLock_RemoveFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: chmod does not prevent file removal")
	}

	tmpDir, err := os.MkdirTemp("", "acquire_remove_fail_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a subdirectory to hold the lock, so we can make it read-only
	subDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(subDir, "test.lock")

	// Create a stale lock file inside the subdirectory
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only so os.Remove fails with a permission error
	if err := os.Chmod(subDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(subDir, 0755) }() // restore for cleanup

	f, err := Acquire(lockPath)

	// The function should still attempt each loop iteration even though Remove fails.
	// In a read-only directory, the lock file still exists after each Remove attempt,
	// and IsStale returns true each time. After maxRetries iterations the function
	// returns with the "failed to acquire lock after N retries" error.
	// We verify the function returns without panicking and returns a non-nil error.
	if err == nil {
		t.Error("expected error when Remove fails in all retry iterations")
	}
	if f != nil {
		t.Error("expected nil file handle when acquisition fails")
	}

	// Restore permissions so cleanup can proceed
	_ = os.Chmod(subDir, 0755)
}

// =============================================================================
// Acquire tests (by lock path — renamed from MetricsManager_AcquireLedgerLock_*)
// =============================================================================

func TestAcquire_ByLockPath_StaleLock_RemoveSucceeds(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "acquire_stale_ok_test2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a stale lock file
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	assertLockAcquiredAndReleased(t, lockPath)
}

func assertLockAcquiredAndReleased(t *testing.T, lockPath string) {
	t.Helper()

	f, err := Acquire(lockPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}

	// Verify the lock file now exists (was re-created)
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("lock file should exist after re-acquire: %v", statErr)
	}

	// Clean up
	Release(lockPath, f)

	// Verify lock was cleaned up
	if _, statErr := os.Stat(lockPath); statErr == nil {
		t.Error("lock file should be removed after cleanup")
	}
}

func TestAcquire_ByLockPath_FreshLock_NotStale(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "acquire_fresh_test2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a fresh lock file (just created, not stale)
	if err := os.WriteFile(lockPath, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := Acquire(lockPath)

	// With retry loop, the error is "failed after N retries", not os.IsExist.
	if err == nil {
		t.Error("expected error for fresh lock after retries, got nil")
	}
	if f != nil {
		t.Error("expected nil file handle for fresh lock conflict")
	}

	// Verify the lock file was NOT removed (it's still fresh/valid)
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Errorf("fresh lock file should still exist: %v", statErr)
	}
}

func TestAcquire_ByLockPath_StaleLock_RemoveFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: chmod does not prevent file removal")
	}

	tmpDir, err := os.MkdirTemp("", "acquire_remove_fail_test2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a subdirectory to hold the lock, so we can make it read-only
	subDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	lockPath := filepath.Join(subDir, "test.lock")

	// Create a stale lock file inside the subdirectory
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only so os.Remove fails with a permission error
	if err := os.Chmod(subDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(subDir, 0755) }() // restore for cleanup

	f, err := Acquire(lockPath)

	// The function should still attempt each loop iteration even though Remove fails.
	// In a read-only directory, the lock file still exists after each Remove attempt,
	// and IsStale returns true each time. After maxRetries iterations the function
	// returns with the "failed to acquire lock after N retries" error.
	// We verify the function returns without panicking and returns a non-nil error.
	if err == nil {
		t.Error("expected error when Remove fails in all retry iterations")
	}
	if f != nil {
		t.Error("expected nil file handle when acquisition fails")
	}

	// Restore permissions so cleanup can proceed
	_ = os.Chmod(subDir, 0755)
}
