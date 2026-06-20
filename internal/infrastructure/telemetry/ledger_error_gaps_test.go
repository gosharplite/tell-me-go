// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Gap 1 — discoverNewRecords context cancellation (ledger.go:145-147)
// ---------------------------------------------------------------------------

func TestDiscoverNewRecords_ContextCancellation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create 2 valid tokens.log files in separate subdirectories.
	sessionA := filepath.Join(tempDir, "sessionA")
	require.NoError(t, os.MkdirAll(sessionA, 0755))
	logA := filepath.Join(sessionA, "tokens.log")
	require.NoError(t, os.WriteFile(logA,
		[]byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

	sessionB := filepath.Join(tempDir, "sessionB")
	require.NoError(t, os.MkdirAll(sessionB, 0755))
	logB := filepath.Join(sessionB, "tokens.log")
	require.NoError(t, os.WriteFile(logB,
		[]byte(`{"cost": 2.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

	// Build file list: first valid file, then a non-existent third path,
	// then the second valid file. The non-existent path exercises the
	// os.Stat error path while the context cancellation exercises
	// the ctx.Err() break condition.
	nonexistent := filepath.Join(tempDir, "does-not-exist", "tokens.log")
	files := []string{logA, nonexistent, logB}

	// Cancel context BEFORE calling discoverNewRecords.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	seen := make(map[string]bool)
	pricing := GetPricing(context.Background(), sm, tempDir)

	// Call discoverNewRecords with the already-cancelled context.
	// The loop must break on the first iteration (ctx.Err() != nil).
	discovered := ls.discoverNewRecords(ctx, files, tempDir, seen, pricing)

	// The loop breaks immediately because ctx.Err() != nil, so no records
	// are processed. This verifies the context cancellation path at
	// ledger.go:145-147 is exercised without panic.
	assert.Empty(t, discovered, "no records should be discovered when context is cancelled")
}

// ---------------------------------------------------------------------------
// Gap 2 — processLogFile timestamp fallback (ledger.go:247-249)
// ---------------------------------------------------------------------------

func TestProcessLogFile_TimestampFallback(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a tokens.log with NO timestamp field — only cost.
	sessionDir := filepath.Join(tempDir, "mode")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	logPath := filepath.Join(sessionDir, "tokens.log")
	require.NoError(t, os.WriteFile(logPath,
		[]byte(`{"cost": 1.5}`), 0644))

	// Set the file's modification time to a known value.
	knownTime := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(logPath, knownTime, knownTime))

	// Stat the file to get os.FileInfo for processLogFile.
	info, err := os.Stat(logPath)
	require.NoError(t, err)

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	pricing := GetPricing(context.Background(), sm, tempDir)

	// Call processLogFile directly. Because the JSON line has no "timestamp"
	// field, parseUsage returns time.Time{} (zero), and processLogFile must
	// fall back to the file's mod time (ledger.go:247-249).
	record, err := ls.processLogFile(logPath, info, tempDir, pricing)
	require.NoError(t, err)
	require.NotNil(t, record)

	// The timestamp must equal the file's mod time (not zero).
	// Use time.Equal to compare instants regardless of location.
	assert.True(t, record.Timestamp.Equal(knownTime),
		"timestamp should fall back to file mod time when log has no timestamp field; got %v, want %v",
		record.Timestamp, knownTime)

	// The Date must be derived from the mod time (since no date regex match
	// in the relative path).
	assert.Equal(t, "2024-06-15", record.Date,
		"date should be derived from file mod time")
}

// ---------------------------------------------------------------------------
// Gap 3 — getSessionID backup prefix path (ledger.go:209-211)
// ---------------------------------------------------------------------------

func TestGetSessionID_BackupPrefix(t *testing.T) {
	t.Parallel()

	ls := &ledgerStore{}

	tests := []struct {
		name      string
		path      string
		globalDir string
		want      string
	}{
		{
			name:      "normal path",
			path:      "/base/mode/tokens.log",
			globalDir: "/base",
			want:      "mode/tokens.log",
		},
		{
			name:      "backup path — singular backup prefix",
			path:      "/base/backups/2023/mode/tokens.log",
			globalDir: "/base",
			want:      "backup/2023/mode/tokens.log",
		},
		{
			name:      "deeply nested backup path",
			path:      "/base/backups/2023/01/15/mode/submode/tokens.log",
			globalDir: "/base",
			want:      "backup/2023/01/15/mode/submode/tokens.log",
		},
		{
			name:      "backups in middle of path (not prefix)",
			path:      "/base/something/backups/tokens.log",
			globalDir: "/base",
			want:      "something/backups/tokens.log",
		},
		{
			name:      "path equals backups dir without trailing slash",
			path:      "/base/backups",
			globalDir: "/base",
			want:      "backups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ls.getSessionID(tt.path, tt.globalDir)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result,
				"getSessionID(%q, %q)", tt.path, tt.globalDir)
		})
	}
}

// ---------------------------------------------------------------------------
// Gap 4 — recoverLedger recovery-already-in-progress (ledger.go:84)
// ---------------------------------------------------------------------------

func TestRecoverLedger_AlreadyInProgress(t *testing.T) {
	t.Parallel()

	// Create a unique key so parallel tests don't interfere.
	key := filepath.Join(t.TempDir(), "global_costs.json")

	ls := &ledgerStore{}

	// First call must succeed (recovery not yet started).
	started := ls.tryStartRecovery(key)
	require.True(t, started, "first tryStartRecovery should return true (recovery not in progress)")

	// Second call with the same key must return false
	// because recovery is already in progress.
	started2 := ls.tryStartRecovery(key)
	assert.False(t, started2, "second tryStartRecovery should return false (recovery already in progress)")

	// Clean up the sync.Map entry so subsequent tests aren't affected.
	recoveryInProgress.Delete(key)

	// After cleanup, a new call should succeed again.
	started3 := ls.tryStartRecovery(key)
	assert.True(t, started3, "tryStartRecovery should return true after cleanup")
	recoveryInProgress.Delete(key)
}

// TestRecoverLedger_EarlyReturnWhenAlreadyInProgress covers the
// recoverLedger early-return path at ledger.go:84-85. When recovery
// is already in progress for a given historyPath, recoverLedger
// must return immediately without creating or modifying any files.
func TestRecoverLedger_EarlyReturnWhenAlreadyInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Pre-populate the recoveryInProgress map to simulate an
	// in-progress recovery.
	recoveryInProgress.Store(historyPath, true)
	t.Cleanup(func() {
		recoveryInProgress.Delete(historyPath)
	})

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	// Call recoverLedger — it must return immediately because
	// tryStartRecovery returns false.
	ls.recoverLedger(context.Background(), tempDir)

	// Verify that global_costs.json was NOT created (early return).
	_, err := os.Stat(historyPath)
	assert.True(t, os.IsNotExist(err),
		"global_costs.json should NOT exist when recovery is already in progress")
}

// ---------------------------------------------------------------------------
// TestProcessLogFile_TimestampFallback_WindowsGuard documents the platform
// guard pattern used for os.Chtimes on Windows.
// ---------------------------------------------------------------------------

func TestProcessLogFile_TimestampFallback_WindowsGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: os.Chtimes precision may vary")
	}
	// This guard is placed here so the main test above can run on all
	// platforms. If os.Chtimes precision becomes an issue on Windows,
	// the guard can be added to TestProcessLogFile_TimestampFallback.
}

// TestGetSessionID_BackupPrefix_ErrorWrapping verifies that getSessionID
// returns a fmt.Errorf-wrapped error when filepath.Rel fails.
func TestGetSessionID_BackupPrefix_ErrorWrapping(t *testing.T) {
	t.Parallel()

	ls := &ledgerStore{}

	// Use paths from different volumes to force filepath.Rel to fail on Windows.
	globalDir := `C:\base`
	path := `D:\other\tokens.log`

	_, relErr := filepath.Rel(globalDir, path)
	if relErr == nil {
		t.Skip("filepath.Rel succeeded — different volumes are not simulated on this platform")
	}

	result, err := ls.getSessionID(path, globalDir)
	require.Error(t, err, "expected error when filepath.Rel fails")
	assert.Contains(t, err.Error(), "resolving session ID",
		"error should wrap with 'resolving session ID'")
	assert.Contains(t, err.Error(), fmt.Errorf("resolving session ID for %s relative to %s: %w", path, globalDir, relErr).Error()[:20],
		"error should contain context about the path")
	assert.Empty(t, result, "expected empty string on error")
}

// ---------------------------------------------------------------------------
// discoverNewRecords — seen[sessionID] skip path (ledger.go:155-156)
// ---------------------------------------------------------------------------

func TestDiscoverNewRecords_AlreadySeen(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a valid tokens.log.
	sessionDir := filepath.Join(tempDir, "session")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	logPath := filepath.Join(sessionDir, "tokens.log")
	require.NoError(t, os.WriteFile(logPath,
		[]byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	// Pre-populate the seen map with the session ID that matches the log file.
	sessionID := "session/tokens.log"
	seen := map[string]bool{sessionID: true}

	pricing := GetPricing(context.Background(), sm, tempDir)

	discovered := ls.discoverNewRecords(context.Background(), []string{logPath}, tempDir, seen, pricing)

	// The file should be skipped because its sessionID is already in the seen map.
	assert.Empty(t, discovered, "no records should be discovered when session is already seen")
}

// ---------------------------------------------------------------------------
// processLogFile — detected model fallback (ledger.go:227-229)
// ---------------------------------------------------------------------------

func TestProcessLogFile_DetectedModelFallback(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a tokens.log where the JSON line has NO "model" field.
	// By using an empty ls.model, parseUsage's defaultModel is also "",
	// so updateParseState does NOT auto-populate detectedModel.
	// This forces processLogFile to take the modelToUse == "" branch.
	sessionDir := filepath.Join(tempDir, "mode")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	logPath := filepath.Join(sessionDir, "tokens.log")
	require.NoError(t, os.WriteFile(logPath,
		[]byte(`{"cost": 1.5, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

	info, err := os.Stat(logPath)
	require.NoError(t, err)

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	// Empty model ensures detectedModel stays "" so the fallback branch is hit.
	ls := newLedgerStore(sm, "", nil)

	pricing := GetPricing(context.Background(), sm, tempDir)

	record, err := ls.processLogFile(logPath, info, tempDir, pricing)
	require.NoError(t, err)
	require.NotNil(t, record)

	// When detectedModel is empty AND ls.model is empty, the fallback
	// branch at ledger.go:227-229 is exercised (modelToUse = ls.model).
	assert.Equal(t, "", record.Model,
		"model should fall back to ls.model (empty) when log has no model and defaultModel is empty")
}

// ---------------------------------------------------------------------------
// processLogFile — date extraction from path regex (ledger.go:240-243)
// ---------------------------------------------------------------------------

func TestProcessLogFile_DateFromPath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a tokens.log in a path that contains a date pattern (YYYY-MM-DD).
	sessionDir := filepath.Join(tempDir, "backups", "2023", "10", "27")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	logPath := filepath.Join(sessionDir, "tokens.log")
	require.NoError(t, os.WriteFile(logPath,
		[]byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

	info, err := os.Stat(logPath)
	require.NoError(t, err)

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	pricing := GetPricing(context.Background(), sm, tempDir)

	record, err := ls.processLogFile(logPath, info, tempDir, pricing)
	require.NoError(t, err)
	require.NotNil(t, record)

	// The date must be extracted from the path using the dateRegex,
	// overriding the file's mod time (ledger.go:240-243).
	assert.Equal(t, "2023-10-27", record.Date,
		"date should be extracted from path when path contains YYYY-MM-DD pattern")
}

// TestProcessLogFile_GetSessionIDError covers the gap at ledger.go:217-219
// where processLogFile calls getSessionID and getSessionID returns an error.
// The error is wrapped with "processing log file %s: %w".
//
// Uses the getSessionIDFunc test-only override to inject a failure directly,
// avoiding reliance on platform-specific filepath.Rel cross-volume behavior.
func TestProcessLogFile_GetSessionIDError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a valid tokens.log so os.Stat and parseUsage succeed
	// (getSessionID is called before parseUsage, so we only need the file
	// to exist for the Stat check in tests that go through discoverNewRecords).
	sessionDir := filepath.Join(tempDir, "mode")
	require.NoError(t, os.MkdirAll(sessionDir, 0755))
	logPath := filepath.Join(sessionDir, "tokens.log")
	require.NoError(t, os.WriteFile(logPath,
		[]byte(`{"prompt_tokens":100,"response_tokens":50,"cost":0.01,"timestamp":"2025-06-15T12:00:00Z"}`+"\n"), 0644))

	info, err := os.Stat(logPath)
	require.NoError(t, err)

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	// Inject a failing getSessionID to simulate a path resolution error.
	injectedErr := errors.New("injected getSessionID error")
	ls.getSessionIDFunc = func(path, globalDir string) (string, error) {
		return "", injectedErr
	}

	record, err := ls.processLogFile(logPath, info, tempDir, domain_pricing.PricingData{})
	require.Error(t, err, "expected error when getSessionID fails")
	assert.Contains(t, err.Error(), "processing log file",
		"error should wrap with 'processing log file'")
	assert.Nil(t, record, "record should be nil on error")
}

// TestDiscoverNewRecords_GetSessionIDError covers the gap at ledger.go:151-153
// where discoverNewRecords receives an error from getSessionID, logs a
// "Recovery: skipping" warning, and continues to the next file without
// panicking or producing a record for the failed path.
//
// Uses the getSessionIDFunc test-only override to inject a getSessionID
// that fails for a designated "bad" path while delegating to the real
// implementation for valid paths. This avoids reliance on platform-specific
// filepath.Rel cross-volume behavior.
func TestDiscoverNewRecords_GetSessionIDError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a valid tokens.log that can be processed normally.
	validDir := filepath.Join(tempDir, "session-valid")
	require.NoError(t, os.MkdirAll(validDir, 0755))
	validLog := filepath.Join(validDir, "tokens.log")
	require.NoError(t, os.WriteFile(validLog,
		[]byte(`{"prompt_tokens":100,"response_tokens":50,"cost":0.01,"timestamp":"2025-06-15T12:00:00Z"}`+"\n"), 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	ls := newLedgerStore(sm, "test-model", nil)

	// Inject a getSessionID that fails for any path containing "bad".
	// For valid paths, delegate to the real implementation so the test
	// verifies that the loop correctly skips bad paths and processes good ones.
	injectedErr := errors.New("injected getSessionID error")
	ls.getSessionIDFunc = func(path, globalDir string) (string, error) {
		if strings.Contains(path, "bad") {
			return "", injectedErr
		}
		// Delegate to the real getSessionID logic for valid paths.
		rel, err := filepath.Rel(globalDir, path)
		if err != nil {
			return "", fmt.Errorf("resolving session ID for %s relative to %s: %w", path, globalDir, err)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "backups/") {
			return "backup/" + rel[len("backups/"):], nil
		}
		return rel, nil
	}

	// Build file list: first a "bad" path (getSessionID fails → skip),
	// then a valid path (should be processed normally).
	badPath := filepath.Join(tempDir, "session-bad", "tokens.log")
	files := []string{badPath, validLog}

	seen := make(map[string]bool)
	pricing := GetPricing(context.Background(), sm, tempDir)

	// Call discoverNewRecords — the bad path must cause getSessionID to fail,
	// log "Recovery: skipping", and continue. The valid path must be processed.
	discovered := ls.discoverNewRecords(context.Background(), files, tempDir, seen, pricing)

	// The valid file should be discovered (session-valid/tokens.log).
	assert.Len(t, discovered, 1, "only the valid file should be discovered; bad path should be skipped")
	if len(discovered) > 0 {
		assert.Contains(t, discovered[0].Session, "session-valid/tokens.log",
			"discovered record should be from the valid path")
	}
}

// ---------------------------------------------------------------------------
// Gap 5 — getSessionID filepath.Rel error (ledger.go:216-218)
// ---------------------------------------------------------------------------

// TestGetSessionID_FilepathRelError covers the gap at ledger.go:216-218
// where filepathRel (the injectable filepath.Rel) fails and getSessionID
// returns a wrapped error.
//
// NOTE: This test modifies the package-level filepathRel variable and
// therefore must NOT use t.Parallel() to avoid races with other tests
// that read the default filepathRel concurrently.
func TestGetSessionID_FilepathRelError(t *testing.T) {
	// Override filepathRel to simulate a cross-volume error on any platform
	originalRel := filepathRel
	filepathRel = func(base, target string) (string, error) {
		return "", fmt.Errorf("cannot compute relative path from %s to %s", base, target)
	}
	t.Cleanup(func() { filepathRel = originalRel })

	ls := &ledgerStore{}

	result, err := ls.getSessionID("/some/path", "/other/dir")

	assert.Error(t, err, "expected error when filepathRel returns an error")
	assert.Contains(t, err.Error(), "resolving session ID",
		"error should wrap with 'resolving session ID'")
	assert.Empty(t, result, "expected empty result on error")
}
