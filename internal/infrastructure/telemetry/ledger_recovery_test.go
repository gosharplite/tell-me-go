// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/require"
)

func TestLedgerRecoveryIntegration(t *testing.T) {
	t.Run("FullRecovery", func(t *testing.T) {
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
		// To truly trigger parseUsage error after Open, we might need something that makes scanner fail.
		// But parseUsage also returns error if it can't open the file (covered by UnreadableLogFile).
		// Let's try to make it skip invalid lines and still work for valid ones.
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
