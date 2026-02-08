// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

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
	// 1. Use t.TempDir() to set up a clean workspace.
	tempDir := t.TempDir()

	// 2. Create two subdirectories representing different modes/sessions.
	sessionADir := filepath.Join(tempDir, "sessionA")
	sessionBDir := filepath.Join(tempDir, "sessionB")
	require.NoError(t, os.MkdirAll(sessionADir, 0755))
	require.NoError(t, os.MkdirAll(sessionBDir, 0755))

	// 3. Write a valid tokens.log into each directory with unique cost data.
	// We use direct "cost" field which ParseUsage will pick up.
	logA := filepath.Join(sessionADir, "tokens.log")
	logB := filepath.Join(sessionBDir, "tokens.log")

	// Timestamp is important for deterministic "Date" in the report
	timestamp := "2023-10-27T10:00:00Z"
	contentA := `{"cost": 1.0, "timestamp": "` + timestamp + `"}`
	contentB := `{"cost": 2.0, "timestamp": "` + timestamp + `"}`

	require.NoError(t, os.WriteFile(logA, []byte(contentA), 0644))
	require.NoError(t, os.WriteFile(logB, []byte(contentB), 0644))

	// 4. Initialize a metricsManager pointing to one of these logs.
	sm := security.NewSecurityManager(strings.NewReader(""))
	sm.RegisterSafePath(tempDir)

	m := &metricsManager{
		sm:      sm,
		logFile: logA,
		model:   "test-model",
		mode:    "sessionA",
		ledger:  NewLedgerStore(sm, "test-model", nil),
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
}
