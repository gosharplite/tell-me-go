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
