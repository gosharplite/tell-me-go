// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// GetDailyCost with backup-directory log file (covers line 89 backup path)
// ---------------------------------------------------------------------------

func TestGetDailyCost_BackupSessionID(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Directory layout matching a backup structure:
	//   <tmpdir>/mode/backups/2023/mode/session_tokens.log
	//   <tmpdir>/mode/backups/2023/global_costs.json
	backupDir := filepath.Join(tempDir, "mode", "backups", "2023", "mode")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	logFile := filepath.Join(backupDir, "session_tokens.log")
	// Write a valid tokens.log so that logFile is not empty and ensureInitialized doesn't panic.
	require.NoError(t, os.WriteFile(logFile, []byte(`{"cost":5.0,"timestamp":"2023-10-27T10:00:00Z"}`+"\n"), 0644))

	// global_costs.json goes into globalDir = filepath.Dir(filepath.Dir(logFile))
	// filepath.Dir(logFile) = <tmpdir>/mode/backups/2023/mode
	// filepath.Dir(...)     = <tmpdir>/mode/backups/2023
	globalDir := filepath.Dir(filepath.Dir(logFile))
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Use a timestamp that is today in UTC-8 so calculateDailyCost includes it.
	now := time.Now()
	records := []sessionCostRecord{
		{
			Session:   "mode/backup/2023/mode/session_tokens.log", // backup-prefixed ID (as written by getSessionID during recovery)
			TotalCost: 5.0,
			Timestamp: now,
		},
	}
	data, err := json.Marshal(records)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(historyPath, data, 0644))

	tracker := &sessionCostTracker{
		totalCost: 0,
		logFile:   logFile,
		mode:      "mode",
	}

	// generateSessionID("mode", logFile) returns "mode/session_tokens.log" (uses filepath.Base only).
	// This differs from the ledger record's "mode/backup/2023/mode/session_tokens.log",
	// so the record is counted as a different session: 5.0 + 0 in-memory = 5.0.
	got := tracker.GetDailyCost(context.Background())
	assert.Equal(t, 5.0, got, "daily cost should include ledger record (5.0) plus in-memory (0.0)")
}

// ---------------------------------------------------------------------------
// generateSessionID with backup paths (direct unit test)
// ---------------------------------------------------------------------------

func TestGenerateSessionID_BackupPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     string
		logFile  string
		expected string
	}{
		{
			name:     "standard path (no backups)",
			mode:     "interactive",
			logFile:  "/data/interactive/2023/10/27/tokens.log",
			expected: "interactive/tokens.log",
		},
		{
			name:     "backup path with backups/ prefix",
			mode:     "mode",
			logFile:  "/tmp/mode/backups/2023/mode/session_tokens.log",
			expected: "mode/session_tokens.log",
		},
		{
			name:     "deeply nested backup path",
			mode:     "automated",
			logFile:  "/var/data/automated/backups/2024/01/15/automated/turn_tokens.log",
			expected: "automated/turn_tokens.log",
		},
		{
			name:     "backup path without mode directory prefix",
			mode:     "mode",
			logFile:  "/tmp/backups/2023/other/session_tokens.log",
			expected: "mode/session_tokens.log",
		},
		{
			name:     "empty mode",
			mode:     "",
			logFile:  "/tmp/backups/2023/empty/tokens.log",
			expected: "tokens.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := generateSessionID(tt.mode, tt.logFile)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// GetDailyCost with empty logFile (covers the early-return branch at line 59)
// ---------------------------------------------------------------------------

func TestGetDailyCost_EmptyLogFile(t *testing.T) {
	t.Parallel()

	tracker := &sessionCostTracker{
		totalCost: 3.0,
		logFile:   "", // empty logFile triggers early return
		mode:      "mode",
	}

	got := tracker.GetDailyCost(context.Background())
	assert.Equal(t, 3.0, got, "empty logFile should return in-memory cost without reading ledger")
}
