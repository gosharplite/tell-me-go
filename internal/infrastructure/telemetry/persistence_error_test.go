// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// persistMergedLedger error-path tests — covering silent data-loss gaps
// =============================================================================

// TestPersistMergedLedger_LockAcquisitionFailure exercises the gap at
// ledger.go:230-232 where a fresh (non-stale) lock file causes
// acquireLedgerLock to return os.IsExist, and persistMergedLedger silently
// returns without writing the ledger.
func TestPersistMergedLedger_LockAcquisitionFailure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Pre-create global_costs.json so we can detect if it was modified
	originalContent := []byte(`[{"date":"2026-01-01","timestamp":"2026-01-01T00:00:00Z","session":"existing","model":"test-model","total_cost":0.5}]`)
	require.NoError(t, os.WriteFile(historyPath, originalContent, 0644))

	// Create a fresh (non-stale) lock file — acquireLedgerLock will get
	// os.IsExist, and isStale returns false for a just-created file.
	require.NoError(t, os.WriteFile(lockPath, []byte("lock"), 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	newRecords := []sessionCostRecord{
		{
			Date:      "2026-02-01",
			Timestamp: time.Now(),
			Session:   "new-session",
			Model:     "test-model",
			TotalCost: 1.0,
		},
	}

	// persistMergedLedger should return silently without modifying the file.
	ls.persistMergedLedger(context.Background(), historyPath, newRecords)

	// Verify global_costs.json was NOT modified.
	content, err := os.ReadFile(historyPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, content, "global_costs.json should NOT have been modified")

	// Verify the fresh lock file was NOT removed (we never acquired it).
	_, err = os.Stat(lockPath)
	assert.NoError(t, err, "fresh lock file should still exist (never acquired)")
}

// TestPersistMergedLedger_AtomicWriteFailure exercises the gap at
// ledger.go:239-241 where AtomicWrite fails (e.g., permission error) and the
// log.Printf warning is emitted. A cancelled context is used to make
// AtomicWrite return early after creating the temp file but before writing.
//
// NOTE: Using a read-only directory to trigger EACCES in AtomicWrite is not
// feasible here because acquireLedgerLock must create a lock file in the
// same directory, which also requires write permission. Using a cancelled
// context exercises the identical error-handling branch.
func TestPersistMergedLedger_AtomicWriteFailure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Pre-create a valid ledger so readExistingRecords parses it correctly.
	require.NoError(t, os.WriteFile(historyPath, []byte(`[]`), 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	newRecords := []sessionCostRecord{
		{
			Date:      "2026-02-01",
			Timestamp: time.Now(),
			Session:   "session-atomic-fail",
			Model:     "test-model",
			TotalCost: 2.5,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	// persistMergedLedger acquires the lock successfully (directory is
	// writable), reads and merges records, marshals JSON, and then calls
	// AtomicWrite — which detects the cancelled context and returns
	// context.Canceled, exercising the log.Printf warning branch.
	ls.persistMergedLedger(ctx, historyPath, newRecords)

	// Verify no panic occurred. The log.Printf output is visible in test logs.
	// The file should still exist with its original content (AtomicWrite
	// cleans up the temp file after cancellation).
	_, err := os.ReadFile(historyPath)
	require.NoError(t, err)
}

// TestPersistMergedLedger_MarshalErrorUnreachable documents that the
// json.Marshal error path in persistMergedLedger (ledger.go:238) is
// UNREACHABLE. All fields in sessionCostRecord are standard JSON types:
// string, time.Time (RFC3339 string), float64, and domain_pricing.UsageStats
// (all int64 fields). None of these can cause json.Marshal to fail.
func TestPersistMergedLedger_MarshalErrorUnreachable(t *testing.T) {
	t.Parallel()

	// Verify sessionCostRecord serializes cleanly.
	records := []sessionCostRecord{
		{
			Date:      "2026-02-01",
			Timestamp: time.Now(),
			Session:   "session-1",
			Model:     "test-model",
			TotalCost: 1.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:     1000,
				ResponseTokens:   500,
				CachedTokens:     100,
				CacheWriteTokens: 50,
				SearchQueries:    2,
				ThinkingTokens:   200,
			},
		},
		{
			Date:      "2026-02-01",
			Timestamp: time.Time{}, // zero value
			Session:   "",
			Model:     "",
			TotalCost: 0,
			Usage:     domain_pricing.UsageStats{}, // all zero
		},
	}

	data, err := json.Marshal(records)
	require.NoError(t, err, "sessionCostRecord must always marshal cleanly")

	// Verify round-trip preserves data.
	var restored []sessionCostRecord
	require.NoError(t, json.Unmarshal(data, &restored))
	assert.Len(t, restored, 2)
	assert.Equal(t, records[0].Session, restored[0].Session)
	assert.Equal(t, records[0].TotalCost, restored[0].TotalCost)
	assert.Equal(t, records[0].Usage.PromptTokens, restored[0].Usage.PromptTokens)

	// Verify the empty/maxed UsageStats also round-trips.
	assert.Equal(t, int64(0), restored[1].Usage.PromptTokens)
	assert.Equal(t, float64(0), restored[1].TotalCost)

	// Verify json.Marshal on a single empty record also works (the minimal
	// path exercised in persistMergedLedger when newRecords is empty).
	data2, err := json.Marshal([]sessionCostRecord{})
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data2))
}

// =============================================================================
// updateLedgerHistory error-path tests — covering silent data-loss gaps
// =============================================================================

// TestUpdateLedgerHistory_AtomicWriteFailure exercises the gap at
// metrics_cost.go:98-100 where AtomicWrite failure is silently logged.
//
// NOTE: The AtomicWrite failure path in updateLedgerHistory is ALREADY
// covered by existing tests (e.g., TestRecordCost_RecoveryContinuesOnContextCancel
// exercises it via recordCost → updateLedgerHistory with a cancelled context).
// This test provides a direct, isolated reproduction using a read-only directory.
//
// NOT parallel — uses os.Chmod.
func TestUpdateLedgerHistory_AtomicWriteFailure(t *testing.T) {
	// Chmod 0555 behaves differently on Windows.
	if runtime.GOOS == "windows" {
		t.Skip("skipping chmod-based test on Windows")
	}

	tempDir := t.TempDir()
	globalDir := tempDir
	outputDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Pre-create a valid ledger file so loadHistory reads it successfully.
	require.NoError(t, os.WriteFile(historyPath, []byte(`[]`), 0644))

	// Make the directory read-only AFTER creating the file.
	// This causes AtomicWrite (which calls CreateTemp in the same directory)
	// to fail with a permission error, exercising the log.Printf warning.
	require.NoError(t, os.Chmod(globalDir, 0555))
	t.Cleanup(func() { _ = os.Chmod(globalDir, 0755) })

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:     sm,
		model:  "test-model",
		mode:   "test-mode",
		ledger: nil, // nil so loadHistory does NOT trigger recovery
	}

	record := sessionCostRecord{
		Date:      time.Now().Format("2006-01-02"),
		Timestamp: time.Now(),
		Session:   "session-ro-test",
		Model:     "test-model",
		TotalCost: 3.0,
	}

	// updateLedgerHistory calls loadHistory (reads file successfully since
	// the directory is readable), upsertRecord, applyRetentionPolicy,
	// json.Marshal, and then AtomicWrite — which fails because the
	// directory is read-only. The warning is logged, and no panic occurs.
	m.updateLedgerHistory(context.Background(), historyPath, globalDir, outputDir, record)

	// If we reach here without panic, the error path was handled gracefully.
	// Restore permissions for cleanup.
	_ = os.Chmod(globalDir, 0755)
}

// =============================================================================
// recordCost lock acquisition failure — same silent data-loss pattern
// =============================================================================

// TestRecordCost_LockAcquisitionFailure exercises the gap at
// metrics_cost.go:35-37 where recordCount silently returns when
// acquireLedgerLock fails with a fresh (non-stale) lock file.
func TestRecordCost_LockAcquisitionFailure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "test-mode")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	// global_costs.json is in the parent of outputDir
	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Pre-create global_costs.json so we can verify it's NOT modified
	originalContent := []byte(`[{"date":"2026-01-01","timestamp":"2026-01-01T00:00:00Z","session":"existing","model":"test-model","total_cost":0.5}]`)
	require.NoError(t, os.WriteFile(historyPath, originalContent, 0644))

	// Create a fresh (non-stale) lock file — acquireLedgerLock will get
	// os.IsExist, isStale returns false, and recordCost silently returns.
	require.NoError(t, os.WriteFile(lockPath, []byte("lock"), 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:    sm,
		model: "test-model",
		mode:  "test-mode",
	}

	record := sessionCostRecord{
		Date:      "2026-02-01",
		Timestamp: time.Now(),
		Session:   "new-session",
		Model:     "test-model",
		TotalCost: 2.0,
	}

	// recordCost should return silently without modifying the ledger.
	m.recordCost(context.Background(), outputDir, "test-mode", record)

	// Verify global_costs.json was NOT modified.
	content, err := os.ReadFile(historyPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, content, "global_costs.json should NOT have been modified")

	// Verify the fresh lock file was NOT removed (we never acquired it).
	_, err = os.Stat(lockPath)
	assert.NoError(t, err, "fresh lock file should still exist (never acquired)")
}

// TestUpdateLedgerHistory_MarshalErrorUnreachable documents that the
// json.Marshal error path in updateLedgerHistory (metrics_cost.go:94-96) is
// UNREACHABLE. The marshaled value is []sessionCostRecord, which contains
// only standard JSON-serializable types. See
// TestPersistMergedLedger_MarshalErrorUnreachable for the detailed proof.
func TestUpdateLedgerHistory_MarshalErrorUnreachable(t *testing.T) {
	t.Parallel()

	// The marshal path in updateLedgerHistory marshals []sessionCostRecord.
	// We already proved in TestPersistMergedLedger_MarshalErrorUnreachable
	// that sessionCostRecord always marshals cleanly. This test verifies
	// the specific shape that updateLedgerHistory produces — a history list
	// that has been through upsertRecord and applyRetentionPolicy.

	// Simulate the exact pipeline: loadHistory → upsertRecord → applyRetentionPolicy
	history := []sessionCostRecord{
		{
			Date:      "2026-01-15",
			Timestamp: time.Now().AddDate(0, 0, -45),
			Session:   "old-session",
			Model:     "test-model",
			TotalCost: 0.5,
		},
		{
			Date:      time.Now().Format("2006-01-02"),
			Timestamp: time.Now(),
			Session:   "recent-session",
			Model:     "test-model",
			TotalCost: 1.0,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   2000,
				ResponseTokens: 1000,
				SearchQueries:  3,
			},
		},
	}

	record := sessionCostRecord{
		Date:      time.Now().Format("2006-01-02"),
		Timestamp: time.Now(),
		Session:   "recent-session",
		Model:     "test-model",
		TotalCost: 1.5, // updated cost
		Usage: domain_pricing.UsageStats{
			PromptTokens:   2500,
			ResponseTokens: 1200,
			SearchQueries:  3,
		},
	}

	// Step 1: upsertRecord
	history = upsertRecord(history, record)

	// Step 2: applyRetentionPolicy (30 days default)
	m := &metricsManager{}
	history = m.applyRetentionPolicy(history, 30)

	// Step 3: json.Marshal — must always succeed
	data, err := json.Marshal(history)
	require.NoError(t, err, "post-pipeline []sessionCostRecord must always marshal cleanly")

	// Verify round-trip.
	var restored []sessionCostRecord
	require.NoError(t, json.Unmarshal(data, &restored))

	// The old session (45 days ago) should be pruned.
	assert.Len(t, restored, 1)
	assert.Equal(t, "recent-session", restored[0].Session)
	assert.Equal(t, float64(1.5), restored[0].TotalCost)
	assert.Equal(t, int64(2500), restored[0].Usage.PromptTokens)
}
