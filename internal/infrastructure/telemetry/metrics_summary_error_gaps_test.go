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

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// getCostSummary error-path gap tests
// =============================================================================

// TestGetCostSummary_AggregateCostsError covers the gap at
// metrics_summary.go:39-40 where aggregateCosts returns an error
// (e.g. invalid interval) and getCostSummary propagates it.
func TestGetCostSummary_AggregateCostsError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Write a valid global_costs.json so ensureLedgerReady succeeds.
	history := []sessionCostRecord{
		{
			Date:      "2026-06-15",
			Timestamp: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			Session:   "mode/tokens.log",
			Model:     "test-model",
			TotalCost: 0.05,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   100,
				ResponseTokens: 50,
			},
		},
	}
	data, err := json.Marshal(history)
	require.NoError(t, err)

	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.NoError(t, os.WriteFile(historyPath, data, 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	m := &metricsManager{
		sm:      sm,
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	// Trigger aggregateCosts error via invalid interval.
	result, err := m.getCostSummary(context.Background(), costSummaryArgs{
		Interval: "month",
	})

	assert.Error(t, err, "expected error for invalid interval")
	assert.Contains(t, err.Error(), "invalid interval", "error should mention invalid interval")
	assert.Empty(t, result, "result should be empty string on error")
}

// TestGetCostSummary_ParseTimeFiltersError covers the gap at
// metrics_summary.go:39-40 where aggregateCosts returns an error
// from parseTimeFilters (invalid start_date) and getCostSummary propagates it.
func TestGetCostSummary_ParseTimeFiltersError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Write a valid global_costs.json so ensureLedgerReady succeeds.
	history := []sessionCostRecord{
		{
			Date:      "2026-06-15",
			Timestamp: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			Session:   "mode/tokens.log",
			Model:     "test-model",
			TotalCost: 0.05,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   100,
				ResponseTokens: 50,
			},
		},
	}
	data, err := json.Marshal(history)
	require.NoError(t, err)

	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.NoError(t, os.WriteFile(historyPath, data, 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	m := &metricsManager{
		sm:      sm,
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	// Trigger parseTimeFilters error via invalid start_date.
	result, err := m.getCostSummary(context.Background(), costSummaryArgs{
		StartDate: "not-a-date",
	})

	assert.Error(t, err, "expected error for invalid start_date")
	assert.Contains(t, err.Error(), "invalid start_date", "error should mention invalid start_date")
	assert.Empty(t, result, "result should be empty string on error")
}

// TestGetCostSummary_EnsureLedgerReadyError covers the gap at
// metrics_summary.go:33-35 where ensureLedgerReady returns a non-nil
// error (corrupted ledger JSON) and getCostSummary propagates it.
func TestGetCostSummary_EnsureLedgerReadyError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Write a corrupted global_costs.json so ensureLedgerReady fails
	// on json.Unmarshal with a non-nil error.
	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.NoError(t, os.WriteFile(historyPath, []byte("this is not valid json"), 0644))

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	m := &metricsManager{
		sm:      sm,
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	result, err := m.getCostSummary(context.Background(), costSummaryArgs{})

	assert.Error(t, err, "expected error for corrupted ledger JSON")
	assert.Contains(t, err.Error(), "invalid character", "error should be the JSON parse error")
	assert.Contains(t, result, "Error parsing cost history", "result should contain the status message")
}

// =============================================================================
// Gap — Silent EstimateCost warning in get_cost_summary handler (metrics.go:109-111)
// =============================================================================

// TestGetCostSummary_SilentEstimateError covers the gap at metrics.go:109-111
// where the get_cost_summary tool handler calls m.EstimateCost silently and
// logs a warning on failure without aborting the summary request.
//
// The handler code is (inside RegisterMetrics):
//
//	if _, err := m.EstimateCost(ctx, true, ""); err != nil {
//	    log.Printf("Warning: Failed to record cost before summary: %v", err)
//	}
//	res, err := m.getCostSummary(ctx, sArgs)
//
// Strategy:
//  1. Create a metricsManager with a logFile path outside all default
//     boundaries (not under CWD, not under TempDir, not registered).
//     This causes EstimateCost -> sm.IsPathSafe to return an error.
//  2. Verify EstimateCost returns an error when the path is not safe.
//  3. Switch to a valid logFile path and verify that getCostSummary still
//     works independently (the silent error does not abort the flow — the
//     handler continues).
func TestGetCostSummary_SilentEstimateError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Write a valid global_costs.json so ensureLedgerReady succeeds.
	history := []sessionCostRecord{
		{
			Date:      "2026-06-15",
			Timestamp: time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
			Session:   "mode/tokens.log",
			Model:     "test-model",
			TotalCost: 0.05,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   100,
				ResponseTokens: 50,
			},
		},
	}
	data, err := json.Marshal(history)
	require.NoError(t, err)

	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.NoError(t, os.WriteFile(historyPath, data, 0644))

	// Create a security manager with NO safe paths registered.
	sm := security.NewSecurityManager(nil)
	// Intentionally do NOT register any path — the logFile will be outside
	// all default boundaries (not under CWD, not under TempDir).

	// Use a logFile path OUTSIDE all default boundaries.
	// This causes IsPathSafe -> ValidatePath -> checkDefaultBoundaries to fail
	// because the path is not under CWD or os.TempDir().
	unregisteredLogFile := "/test-not-in-boundary/session_tokens.log"
	m := &metricsManager{
		sm:      sm,
		logFile: unregisteredLogFile,
		model:   "test-model",
		mode:    "test-mode",
		ledger:  nil,
	}

	// Step 1: Verify EstimateCost fails because the log path is not safe.
	_, err = m.EstimateCost(context.Background(), true, "")
	require.Error(t, err, "EstimateCost should fail when log path is outside all boundaries")
	assert.Contains(t, err.Error(), "security violation",
		"error should indicate path safety violation")

	// Step 2: Simulate the handler flow — the error is logged (not returned),
	// and getCostSummary still executes independently.
	//
	// In the real handler:
	//   if _, err := m.EstimateCost(ctx, true, ""); err != nil {
	//       log.Printf("Warning: Failed to record cost before summary: %v", err)
	//   }
	//   res, err := m.getCostSummary(ctx, sArgs)
	//
	// We verify that getCostSummary works fine despite EstimateCost failing.

	// Switch m.logFile to the tempDir path where global_costs.json lives.
	// getCostSummary computes historyPath from m.logFile:
	//   outputDir = filepath.Dir(m.logFile)  -> .../mode
	//   globalDir = filepath.Dir(outputDir)  -> tempDir
	//   historyPath = globalDir/global_costs.json  -> the file we wrote above
	m.logFile = filepath.Join(tempDir, "mode", "session_tokens.log")

	// Register tempDir as a safe path (belt-and-suspenders; getCostSummary's
	// ensureLedgerReady does not call IsPathSafe directly, but this matches
	// real-world usage where the path would be registered).
	sm.RegisterSafePath(tempDir)

	result, err := m.getCostSummary(context.Background(), costSummaryArgs{})
	require.NoError(t, err, "getCostSummary should succeed independently of EstimateCost failure")
	assert.NotEmpty(t, result, "getCostSummary should return a result string")
	assert.Contains(t, result, "Total Cost", "result should contain cost summary data")
}
