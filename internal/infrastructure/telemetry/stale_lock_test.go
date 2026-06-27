// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

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

	// 2. isStale should be false if it's new
	if isStale(lockPath) {
		t.Error("isStale returned true for a fresh lock")
	}

	// 3. Make it old
	oldTime := time.Now().Add(-10 * time.Minute)
	err = os.Chtimes(lockPath, oldTime, oldTime)
	if err != nil {
		t.Fatal(err)
	}

	// 4. isStale should now be true
	if !isStale(lockPath) {
		t.Error("isStale returned false for a stale lock")
	}
}

func TestRecordCost_BreaksStaleLock(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "record_cost_stale_lock_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputDir := filepath.Join(tmpDir, "coder")
	_ = os.MkdirAll(outputDir, 0755)

	globalDir := tmpDir
	historyPath := filepath.Join(globalDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Create a stale lock
	_ = os.WriteFile(lockPath, []byte("stale"), 0644)
	oldTime := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(lockPath, oldTime, oldTime)

	m := &metricsManager{
		sm: security.NewSecurityManager(nil),
	}

	record := sessionCostRecord{
		Date:      "2026-02-02",
		Session:   "test-session",
		Model:     "test-model",
		TotalCost: 1.23,
	}

	// recordCost should succeed because it breaks the stale lock
	m.recordCost(context.Background(), outputDir, "coder", record)

	// Verify the lock is gone (removed after success)
	if _, err := os.Stat(lockPath); err == nil {
		t.Error("Lock file still exists after recordCost")
	}

	// Verify history was written
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		t.Error("history file was not written")
	}
}

func TestRecoverLedger_DetectedModel(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "recover_ledger_model_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	globalDir := tmpDir
	sessionDir := filepath.Join(globalDir, "coder", "20260202_120000")
	_ = os.MkdirAll(sessionDir, 0755)

	logPath := filepath.Join(sessionDir, "tokens.log")

	// Create a log with a specific model in JSON
	metrics := llm.Metrics{
		Model:          "gpt-4-special",
		CachedTokens:   100,
		PromptTokens:   200,
		ResponseTokens: 50,
	}
	data, _ := json.Marshal(metrics)
	_ = os.WriteFile(logPath, append(data, '\n'), 0644)

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{
		sm:    sm,
		model: "default-model", // This is the current session model
	}

	// We need a pricing override for "gpt-4-special" to test recalculation
	m.pricingOverrides = map[string]domain_pricing.ModelPricing{
		"gpt-4-special": {
			Hit:  10.0,
			Miss: 100.0,
			Comp: 200.0,
		},
	}
	m.ledger = newLedgerStore(sm, m.model, m.pricingOverrides)

	m.ledger.recoverLedger(context.Background(), globalDir)

	// Wait for background recovery (though here it might be sync if it's small)
	// Actually recoverLedger is sync when called directly like this, but wait, it uses a sync.Map to prevent double recovery.

	historyPath := filepath.Join(globalDir, "global_costs.json")
	content, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}

	var history []sessionCostRecord
	_ = json.Unmarshal(content, &history)

	if len(history) == 0 {
		t.Fatal("History not recovered")
	}

	found := false
	for _, r := range history {
		if r.Model == "gpt-4-special" {
			found = true
			// Cost for gpt-4-special:
			// Hit: 100 * 10 / 1e6 = 0.001
			// Miss: (200-100) * 100 / 1e6 = 0.01
			// Comp: 50 * 200 / 1e6 = 0.01
			// Total: 0.021
			if r.TotalCost == 0 {
				t.Errorf("Recovered cost is 0")
			}
			// Let's check exact cost if possible
			expected := (100.0*10.0 + 100.0*100.0 + 50.0*200.0) / 1e6
			if math.Abs(r.TotalCost-expected) > 1e-9 {
				t.Errorf("Expected cost %f, got %f", expected, r.TotalCost)
			}
		}
	}

	if !found {
		t.Error("Recovered record has wrong model or was not found")
	}
}

// =============================================================================
// releaseLedgerLock error path tests
// =============================================================================

func TestReleaseLedgerLock_CloseError(t *testing.T) {
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

	// Close the underlying file descriptor BEFORE calling releaseLedgerLock.
	// The subsequent f.Close() inside releaseLedgerLock will return an error
	// (double-close / use of closed file).
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	ls := newLedgerStore(nil, "test-model", nil)

	// releaseLedgerLock must not panic when Close returns an error,
	// and os.Remove must still execute (lock file cleaned up).
	ls.releaseLedgerLock(historyPath, f)

	// Verify lock file was removed despite Close error
	if _, err := os.Stat(lockPath); err == nil {
		t.Error("lock file should have been removed even when Close fails")
	}
}

func TestReleaseLedgerLock_RemoveError(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "release_lock_remove_error_test")
	if err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(tmpDir, "global_costs.json")
	lockPath := historyPath + ".lock"

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

	ls := newLedgerStore(nil, "test-model", nil)

	// releaseLedgerLock must not panic when os.Remove fails with a
	// non-NotExist error (permission denied).
	ls.releaseLedgerLock(historyPath, f)

	// If we reach here without panic, the error path was handled correctly.
}

func TestReleaseLedgerLock_NilFile(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "release_lock_nil_file_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	historyPath := filepath.Join(tmpDir, "global_costs.json")

	ls := newLedgerStore(nil, "test-model", nil)

	// releaseLedgerLock must not panic when f is nil.
	// The lock file may or may not exist — the test only asserts no panic.
	ls.releaseLedgerLock(historyPath, nil)

	// Additionally verify it doesn't panic when the lock file also doesn't exist
	ls.releaseLedgerLock(historyPath, nil)
}

// =============================================================================
// ledgerStore.acquireLedgerLock tests
// =============================================================================

func TestLedgerStore_AcquireLedgerLock_StaleLock_RemoveSucceeds(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "ls_acquire_stale_ok_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	historyPath := filepath.Join(tmpDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Create a stale lock file
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	ls := newLedgerStore(nil, "test-model", nil)

	// acquireLedgerLock should break the stale lock and re-acquire
	f, err := acquireLedgerLock(historyPath + ".lock")
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
	ls.releaseLedgerLock(historyPath, f)

	// Verify lock was cleaned up
	if _, statErr := os.Stat(lockPath); statErr == nil {
		t.Error("lock file should be removed after release")
	}
}

func TestLedgerStore_AcquireLedgerLock_FreshLock_NotStale(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "ls_acquire_fresh_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	historyPath := filepath.Join(tmpDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Create a fresh lock file (just created, not stale)
	if err := os.WriteFile(lockPath, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := acquireLedgerLock(historyPath + ".lock")

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

func TestLedgerStore_AcquireLedgerLock_StaleLock_RemoveFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: chmod does not prevent file removal")
	}

	tmpDir, err := os.MkdirTemp("", "ls_acquire_remove_fail_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a subdirectory to hold the lock, so we can make it read-only
	subDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(subDir, "global_costs.json")
	lockPath := historyPath + ".lock"

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

	f, err := acquireLedgerLock(historyPath + ".lock")

	// The function should still attempt each loop iteration even though Remove fails.
	// In a read-only directory, the lock file still exists after each Remove attempt,
	// and isStale returns true each time. After maxRetries iterations the function
	// returns with the "failed to acquire ledger lock after N retries" error.
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
// metricsManager.acquireLedgerLock tests
// =============================================================================

func TestMetricsManager_AcquireLedgerLock_StaleLock_RemoveSucceeds(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "mm_acquire_stale_ok_test")
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

	f, err := acquireLedgerLock(lockPath)
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
	if closeErr := f.Close(); closeErr != nil {
		t.Logf("close error (non-fatal): %v", closeErr)
	}
	if removeErr := os.Remove(lockPath); removeErr != nil {
		t.Logf("remove error (non-fatal): %v", removeErr)
	}

	// Verify lock was cleaned up
	if _, statErr := os.Stat(lockPath); statErr == nil {
		t.Error("lock file should be removed after cleanup")
	}
}

func TestMetricsManager_AcquireLedgerLock_FreshLock_NotStale(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "mm_acquire_fresh_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	lockPath := filepath.Join(tmpDir, "test.lock")

	// Create a fresh lock file (just created, not stale)
	if err := os.WriteFile(lockPath, []byte("fresh"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := acquireLedgerLock(lockPath)

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

func TestMetricsManager_AcquireLedgerLock_StaleLock_RemoveFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows: chmod does not prevent file removal")
	}

	tmpDir, err := os.MkdirTemp("", "mm_acquire_remove_fail_test")
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

	f, err := acquireLedgerLock(lockPath)

	// The function should still attempt each loop iteration even though Remove fails.
	// In a read-only directory, the lock file still exists after each Remove attempt,
	// and isStale returns true each time. After maxRetries iterations the function
	// returns with the "failed to acquire ledger lock after N retries" error.
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
