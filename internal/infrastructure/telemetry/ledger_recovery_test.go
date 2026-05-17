// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLedgerRecoveryIntegration(t *testing.T) {
	t.Parallel()
	t.Run("FullRecovery", func(t *testing.T) {
		t.Parallel()
		// 1. Use t.TempDir() to set up a clean workspace.

		tempDir := t.TempDir()

		// 2. Create two subdirectories representing different modes/sessions.
		sessionADir := filepath.Join(tempDir, "sessionA")
		sessionBDir := filepath.Join(tempDir, "sessionB")
		require.NoError(t, os.MkdirAll(sessionADir, 0755))
		require.NoError(t, os.MkdirAll(sessionBDir, 0755))

		// 3. Write a valid tokens.log into each directory with unique cost data.
		// We use direct "cost" field which parseUsage will pick up.
		logA := filepath.Join(sessionADir, "tokens.log")
		logB := filepath.Join(sessionBDir, "tokens.log")

		// Timestamp is important for deterministic "Date" in the report
		timestamp := "2023-10-27T10:00:00Z"
		contentA := `{"cost": 1.0, "timestamp": "` + timestamp + `"}`
		contentB := `{"cost": 2.0, "timestamp": "` + timestamp + `"}`

		require.NoError(t, os.WriteFile(logA, []byte(contentA), 0644))
		require.NoError(t, os.WriteFile(logB, []byte(contentB), 0644))

		// 4. Initialize a metricsManager pointing to one of these logs.
		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)

		m := &metricsManager{
			sm:      sm,
			logFile: logA,
			model:   "test-model",
			mode:    "sessionA",
			ledger:  newLedgerStore(sm, "test-model", nil),
		}

		// 5. Ensure NO global_costs.json exists initially.
		historyPath := filepath.Join(tempDir, "global_costs.json")
		_, err := os.Stat(historyPath)
		require.True(t, os.IsNotExist(err), "global_costs.json should not exist initially")

		// 6. Call getCostSummary(ctx, costSummaryArgs{Billing: false}).
		// This should trigger async recovery.
		ctx := context.Background()
		initialSummary, err := m.getCostSummary(ctx, costSummaryArgs{Billing: false})
		require.NoError(t, err)
		require.Contains(t, initialSummary, "Cost history ledger is missing")

		// 7. Assertion: Implement a require.Eventually or simple polling loop.
		require.Eventually(t, func() bool {
			summary, err := m.getCostSummary(ctx, costSummaryArgs{Billing: false})
			if err != nil {
				return false
			}
			// If recovery is still in progress, it returns a specific message.
			if strings.Contains(summary, "recovery is currently in progress") {
				return false
			}
			if strings.Contains(summary, "is missing") {
				return false
			}

			// Verify the "Grand Total" is $3.0000.
			// Expected format: "| **Grand Total** | **0** | **0** | **0** | **0.0%** | **$3.0000** |"
			return strings.Contains(summary, "**$3.0000**")
		}, 2*time.Second, 100*time.Millisecond, "Ledger recovery should reconstruct the history with total $3.0000")

		// Additional check: Ensure global_costs.json was actually created
		require.FileExists(t, historyPath)
	})

	t.Run("CorruptedLedger", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		historyPath := filepath.Join(tempDir, "global_costs.json")
		require.NoError(t, os.WriteFile(historyPath, []byte("{broken}"), 0644))

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		// Create a valid log file to see if recovery continues
		sessionDir := filepath.Join(tempDir, "session")
		require.NoError(t, os.MkdirAll(sessionDir, 0755))
		logFile := filepath.Join(sessionDir, "tokens.log")
		require.NoError(t, os.WriteFile(logFile, []byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

		ls.recoverLedger(context.Background(), tempDir)

		// Verification: The ledger should be rewritten with the new record,
		// ignoring the corrupted one.
		content, err := os.ReadFile(historyPath)
		require.NoError(t, err)
		require.Contains(t, string(content), "session/tokens.log")
		// JSON marshal of 1.0 might be 1
		require.True(t, strings.Contains(string(content), "\"total_cost\":1") || strings.Contains(string(content), "\"total_cost\":1.0"))
	})

	t.Run("UnreadableLogFile", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Skipping on Windows: os.Chmod(0000) does not make files unreadable for the owner.")
		}
		t.Parallel()
		tempDir := t.TempDir()
		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		// 1. Unreadable log file
		unreadableDir := filepath.Join(tempDir, "unreadable")
		require.NoError(t, os.MkdirAll(unreadableDir, 0755))
		unreadableLog := filepath.Join(unreadableDir, "tokens.log")
		require.NoError(t, os.WriteFile(unreadableLog, []byte(`{}`), 0000))

		// 2. Readable log file
		readableDir := filepath.Join(tempDir, "readable")
		require.NoError(t, os.MkdirAll(readableDir, 0755))
		readableLog := filepath.Join(readableDir, "tokens.log")
		require.NoError(t, os.WriteFile(readableLog, []byte(`{"cost": 2.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

		ls.recoverLedger(context.Background(), tempDir)

		historyPath := filepath.Join(tempDir, "global_costs.json")
		content, err := os.ReadFile(historyPath)
		require.NoError(t, err)
		// Should contain the readable record
		require.Contains(t, string(content), "readable/tokens.log")
		require.True(t, strings.Contains(string(content), "\"total_cost\":2") || strings.Contains(string(content), "\"total_cost\":2.0"))
		// Should NOT contain the unreadable one
		require.NotContains(t, string(content), "unreadable/tokens.log")
	})

	t.Run("InvalidLogContent", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		// 1. Log file with some invalid JSON and some valid JSON
		mixedDir := filepath.Join(tempDir, "mixed")
		require.NoError(t, os.MkdirAll(mixedDir, 0755))
		mixedLog := filepath.Join(mixedDir, "tokens.log")
		// First line invalid, second line valid
		require.NoError(t, os.WriteFile(mixedLog, []byte("{invalid}\n{\"cost\": 3.0, \"timestamp\": \"2023-10-27T10:00:00Z\"}"), 0644))

		ls.recoverLedger(context.Background(), tempDir)

		historyPath := filepath.Join(tempDir, "global_costs.json")
		content, err := os.ReadFile(historyPath)
		require.NoError(t, err)
		// Should contain the record from the valid part of the mixed log
		require.Contains(t, string(content), "mixed/tokens.log")
		require.True(t, strings.Contains(string(content), "\"total_cost\":3") || strings.Contains(string(content), "\"total_cost\":3.0"))
	})
}

// ---------------------------------------------------------------------------
// loadExistingSessionIDs extended test (Phase 6)
// ---------------------------------------------------------------------------

func TestLoadExistingSessionIDs_InvalidJSON(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Write invalid JSON that cannot be unmarshalled.
	if err := os.WriteFile(historyPath, []byte("this is not valid json {{{"), 0644); err != nil {
		t.Fatal(err)
	}

	ls := &ledgerStore{}
	seen := ls.loadExistingSessionIDs(historyPath)

	if len(seen) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %d entries", len(seen))
	}
}

func TestLoadExistingSessionIDs_ValidJSON(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Write valid JSON with multiple session records.
	records := []sessionCostRecord{
		{Session: "s1", TotalCost: 1.0},
		{Session: "s2", TotalCost: 2.0},
		{Session: "s3", TotalCost: 3.0},
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	ls := &ledgerStore{}
	seen := ls.loadExistingSessionIDs(historyPath)

	if len(seen) != 3 {
		t.Errorf("expected 3 entries, got %d", len(seen))
	}
	for _, r := range records {
		if !seen[r.Session] {
			t.Errorf("expected session %q to be in map", r.Session)
		}
	}
}

// ---------------------------------------------------------------------------
// mergeRecords duplicate key test (Phase 7)
// ---------------------------------------------------------------------------

func TestMergeRecords_DuplicateKey(t *testing.T) {
	t.Parallel()

	ls := &ledgerStore{}

	history := []sessionCostRecord{
		{Session: "unique-old", TotalCost: 0.5},
		{Session: "same-session", TotalCost: 1.0},
	}

	newRecords := []sessionCostRecord{
		{Session: "same-session", TotalCost: 2.0},
	}

	merged := ls.mergeRecords(history, newRecords)

	if len(merged) != 2 {
		t.Errorf("expected 2 merged records, got %d", len(merged))
	}

	// Find the "same-session" record and verify new wins.
	found := false
	for _, r := range merged {
		if r.Session == "same-session" {
			found = true
			if r.TotalCost != 2.0 {
				t.Errorf("expected TotalCost 2.0 for same-session (new wins), got %f", r.TotalCost)
			}
		}
	}
	if !found {
		t.Error("expected 'same-session' in merged result")
	}

	// Verify unique-old is still there.
	foundOld := false
	for _, r := range merged {
		if r.Session == "unique-old" {
			foundOld = true
			if r.TotalCost != 0.5 {
				t.Errorf("expected TotalCost 0.5 for unique-old, got %f", r.TotalCost)
			}
		}
	}
	if !foundOld {
		t.Error("expected 'unique-old' in merged result")
	}
}

// ---------------------------------------------------------------------------
// findLogFiles error paths (Phase 8)
// ---------------------------------------------------------------------------

func TestFindLogFiles_ErrorPaths(t *testing.T) {
	// Subtest A: inaccessible subdirectory — exercises ledger.go:179-180
	// (inner WalkDirFunc access error path)
	t.Run("inaccessible subdirectory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod 0000 not effective on Windows")
		}

		globalDir := t.TempDir()

		// Create an inaccessible subdirectory with a valid tokens.log inside.
		// When WalkDir tries to read the subdirectory's contents, it will
		// invoke the WalkDirFunc with a permission error.
		inaccessibleDir := filepath.Join(globalDir, "inaccessible")
		require.NoError(t, os.MkdirAll(inaccessibleDir, 0755))
		inaccessibleLog := filepath.Join(inaccessibleDir, "tokens.log")
		require.NoError(t, os.WriteFile(inaccessibleLog, []byte(`{"cost": 1.0}`), 0644))
		require.NoError(t, os.Chmod(inaccessibleDir, 0000))
		t.Cleanup(func() {
			_ = os.Chmod(inaccessibleDir, 0755)
		})

		// Create a readable subdirectory with a valid tokens.log.
		readableDir := filepath.Join(globalDir, "readable")
		require.NoError(t, os.MkdirAll(readableDir, 0755))
		readableLog := filepath.Join(readableDir, "tokens.log")
		require.NoError(t, os.WriteFile(readableLog, []byte(`{"cost": 2.0}`), 0644))

		ls := &ledgerStore{}
		files, err := ls.findLogFiles(globalDir)

		// Walk errors are now collected and returned via errors.Join.
		// We expect a non-nil error because of the permission-denied subdirectory.
		require.Error(t, err, "findLogFiles should return an error for inaccessible subdirectory")
		require.Contains(t, err.Error(), "walk errors during recovery")

		// The readable tokens.log should be found (walk continues despite errors).
		found := false
		for _, f := range files {
			if f == readableLog {
				found = true
				break
			}
		}
		require.True(t, found, "expected readable tokens.log to be in the returned list")

		// The inaccessible tokens.log should NOT be found.
		for _, f := range files {
			if f == inaccessibleLog {
				t.Errorf("expected inaccessible tokens.log to NOT be in the returned list")
			}
		}
	})

	// Subtest B: non-existent root directory — exercises ledger.go:91-93
	// (outer filepath.WalkDir error path).
	// NOTE: The WalkDirFunc's os.IsNotExist guard (line 178) catches
	// fs.ErrNotExist for the root as well, so WalkDir returns nil.
	// This test documents the current behavior; if the guard is narrowed
	// to only skip missing entries inside an existing root, this test
	// should be updated to expect a non-nil error.
	t.Run("non-existent root directory", func(t *testing.T) {
		ls := &ledgerStore{}
		nonexistentDir := filepath.Join(t.TempDir(), "nonexistent")
		files, err := ls.findLogFiles(nonexistentDir)

		// Current behavior: os.IsNotExist in WalkDirFunc swallows the
		// fs.ErrNotExist from the root, so no error is returned.
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("expected empty files, got %d", len(files))
		}
	})
}

// ---------------------------------------------------------------------------
// discoverNewRecords os.Stat error path (Phase 9)
// ---------------------------------------------------------------------------

func TestDiscoverNewRecords_OsStatError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// 1. Create a valid log file
	validDir := filepath.Join(tempDir, "valid")
	require.NoError(t, os.MkdirAll(validDir, 0755))
	validLog := filepath.Join(validDir, "tokens.log")
	require.NoError(t, os.WriteFile(validLog, []byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

	// 2. Create a second log file, then delete it
	goneDir := filepath.Join(tempDir, "gone")
	require.NoError(t, os.MkdirAll(goneDir, 0755))
	goneLog := filepath.Join(goneDir, "tokens.log")
	require.NoError(t, os.WriteFile(goneLog, []byte(`{"cost": 2.0}`), 0644))
	require.NoError(t, os.Remove(goneLog)) // <-- file removed before stat

	// 3. Build a file list that includes the deleted file
	files := []string{validLog, goneLog}

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	seen := make(map[string]bool)
	pricing := GetPricing(context.Background(), sm, tempDir)

	// 4. Call discoverNewRecords directly
	discovered := ls.discoverNewRecords(context.Background(), files, tempDir, seen, pricing)

	// 5. Assertions
	require.Len(t, discovered, 1, "only the valid file should be discovered")
	assert.Contains(t, discovered[0].Session, "valid/tokens.log")
	// The goneLog os.Stat error path is exercised silently (continue)
}

// ---------------------------------------------------------------------------
// getSessionID filepath.Rel fallback error path (Phase 10)
// ---------------------------------------------------------------------------

func TestGetSessionID_RelFallback(t *testing.T) {
	t.Parallel()

	ls := &ledgerStore{}

	t.Run("normal relative path", func(t *testing.T) {
		result, err := ls.getSessionID("/base/session/tokens.log", "/base")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "session/tokens.log" {
			t.Errorf("expected 'session/tokens.log', got %q", result)
		}
	})

	t.Run("backup prefix rewriting", func(t *testing.T) {
		result, err := ls.getSessionID("/base/backups/2023/session/tokens.log", "/base")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "backup/2023/session/tokens.log" {
			t.Errorf("expected 'backup/2023/session/tokens.log', got %q", result)
		}
	})

	t.Run("rel error returns error", func(t *testing.T) {
		globalDir := `C:\base`
		path := `D:\other\tokens.log`

		rel, relErr := filepath.Rel(globalDir, path)
		if relErr != nil {
			result, err := ls.getSessionID(path, globalDir)
			if err == nil {
				t.Error("expected error from getSessionID when filepath.Rel fails")
			} else if !strings.Contains(err.Error(), "resolving session ID") {
				t.Errorf("expected error to contain 'resolving session ID', got: %v", err)
			}
			if result != "" {
				t.Errorf("expected empty string on error, got %q", result)
			}
		} else {
			t.Logf("filepath.Rel succeeded (expected on Unix): rel=%q", rel)
			result, err := ls.getSessionID(path, globalDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != filepath.ToSlash(rel) {
				t.Errorf("expected %q, got %q", filepath.ToSlash(rel), result)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestFindLogFiles_WalkError (Phase 11 — Recovery failure hardening)
// ---------------------------------------------------------------------------

func TestFindLogFiles_WalkError(t *testing.T) {
	t.Run("walk succeeds cleanly", func(t *testing.T) {
		t.Parallel()
		globalDir := t.TempDir()

		// Create a normal directory with tokens.log files
		sessionA := filepath.Join(globalDir, "sessionA")
		require.NoError(t, os.MkdirAll(sessionA, 0755))
		logA := filepath.Join(sessionA, "tokens.log")
		require.NoError(t, os.WriteFile(logA, []byte(`{"cost": 1.0}`), 0644))

		sessionB := filepath.Join(globalDir, "sessionB")
		require.NoError(t, os.MkdirAll(sessionB, 0755))
		logB := filepath.Join(sessionB, "tokens.log")
		require.NoError(t, os.WriteFile(logB, []byte(`{"cost": 2.0}`), 0644))

		ls := &ledgerStore{}
		files, err := ls.findLogFiles(globalDir)

		require.NoError(t, err, "expected nil error for clean walk")
		require.Len(t, files, 2, "expected both tokens.log files to be found")
		assert.Contains(t, files, logA)
		assert.Contains(t, files, logB)
	})

	t.Run("walk permission denied mid-tree", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod 0000 not effective on Windows")
		}
		t.Parallel()
		globalDir := t.TempDir()

		// Create an inaccessible subdirectory with a tokens.log inside
		inaccessibleDir := filepath.Join(globalDir, "locked")
		require.NoError(t, os.MkdirAll(inaccessibleDir, 0755))
		inaccessibleLog := filepath.Join(inaccessibleDir, "tokens.log")
		require.NoError(t, os.WriteFile(inaccessibleLog, []byte(`{"cost": 3.0}`), 0644))
		require.NoError(t, os.Chmod(inaccessibleDir, 0000))
		t.Cleanup(func() {
			_ = os.Chmod(inaccessibleDir, 0755)
		})

		// Create a readable directory with a tokens.log
		readableDir := filepath.Join(globalDir, "open")
		require.NoError(t, os.MkdirAll(readableDir, 0755))
		readableLog := filepath.Join(readableDir, "tokens.log")
		require.NoError(t, os.WriteFile(readableLog, []byte(`{"cost": 4.0}`), 0644))

		ls := &ledgerStore{}
		files, err := ls.findLogFiles(globalDir)

		require.Error(t, err, "expected error from permission-denied subdirectory")
		require.Contains(t, err.Error(), "walk errors during recovery")

		// The accessible file should still be found
		found := false
		for _, f := range files {
			if f == readableLog {
				found = true
				break
			}
		}
		require.True(t, found, "expected accessible tokens.log to be in the returned list")

		// The inaccessible file should NOT be found
		for _, f := range files {
			require.NotEqual(t, inaccessibleLog, f, "inaccessible tokens.log should not be listed")
		}
	})

	t.Run("walk dir not found", func(t *testing.T) {
		t.Parallel()
		ls := &ledgerStore{}
		nonexistentDir := filepath.Join(t.TempDir(), "does-not-exist")
		files, err := ls.findLogFiles(nonexistentDir)

		// IsNotExist for the root is swallowed by the WalkDirFunc guard
		require.NoError(t, err, "expected nil error for non-existent root")
		require.Empty(t, files, "expected no files for non-existent root")
	})
}

// ---------------------------------------------------------------------------
// TestGetSessionID_RelFailure (Phase 11 — Recovery failure hardening)
// ---------------------------------------------------------------------------

func TestGetSessionID_RelFailure(t *testing.T) {
	t.Parallel()

	ls := &ledgerStore{}

	t.Run("relative path resolves normally", func(t *testing.T) {
		result, err := ls.getSessionID("/base/session/tokens.log", "/base")
		require.NoError(t, err)
		assert.Equal(t, "session/tokens.log", result)
	})

	t.Run("different volume on Windows", func(t *testing.T) {
		// Use paths from different roots to force filepath.Rel to fail
		globalDir := `C:\base`
		path := `D:\other\tokens.log`

		_, relErr := filepath.Rel(globalDir, path)
		if relErr == nil {
			t.Skip("filepath.Rel succeeded — different volumes are not simulated on this platform")
		}

		result, err := ls.getSessionID(path, globalDir)
		require.Error(t, err, "expected error when filepath.Rel fails")
		assert.Contains(t, err.Error(), "resolving session ID")
		assert.Empty(t, result, "expected empty string on error")
	})
}

// ---------------------------------------------------------------------------
// TestRecoverLedger_PartialWalk (Phase 11 — Recovery failure hardening)
// ---------------------------------------------------------------------------

func TestRecoverLedger_PartialWalk(t *testing.T) {
	t.Run("partial walk with some files", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod 0000 not effective on Windows")
		}
		t.Parallel()
		tempDir := t.TempDir()

		// Create an inaccessible subdirectory so findLogFiles returns partial + error
		inaccessibleDir := filepath.Join(tempDir, "locked")
		require.NoError(t, os.MkdirAll(inaccessibleDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(inaccessibleDir, "tokens.log"),
			[]byte(`{"cost": 99.0}`), 0644))
		require.NoError(t, os.Chmod(inaccessibleDir, 0000))
		t.Cleanup(func() {
			_ = os.Chmod(inaccessibleDir, 0755)
		})

		// Create a readable directory with a valid tokens.log
		readableDir := filepath.Join(tempDir, "open")
		require.NoError(t, os.MkdirAll(readableDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(readableDir, "tokens.log"),
			[]byte(`{"cost": 5.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		ls.recoverLedger(context.Background(), tempDir)

		historyPath := filepath.Join(tempDir, "global_costs.json")
		require.FileExists(t, historyPath, "global_costs.json should have been created from partial recovery")

		content, err := os.ReadFile(historyPath)
		require.NoError(t, err)
		// The accessible file's data should be in the recovered ledger
		assert.Contains(t, string(content), "open/tokens.log")
		require.True(t, strings.Contains(string(content), "\"total_cost\":5") ||
			strings.Contains(string(content), "\"total_cost\":5.0"))
	})

	t.Run("walk returns zero files with error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod 0000 not effective on Windows")
		}
		t.Parallel()
		tempDir := t.TempDir()

		// Create ONLY an inaccessible subdirectory — no readable tokens.log at all.
		// findLogFiles will return (nil, walkError) because: WalkDir fails on the
		// inaccessible directory and no tokens.log files are found anywhere.
		inaccessibleDir := filepath.Join(tempDir, "locked")
		require.NoError(t, os.MkdirAll(inaccessibleDir, 0755))
		require.NoError(t, os.Chmod(inaccessibleDir, 0000))
		t.Cleanup(func() {
			_ = os.Chmod(inaccessibleDir, 0755)
		})

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		ls.recoverLedger(context.Background(), tempDir)

		historyPath := filepath.Join(tempDir, "global_costs.json")
		_, err := os.Stat(historyPath)
		require.True(t, os.IsNotExist(err), "global_costs.json should NOT be created when walk returns zero files")
	})
}
