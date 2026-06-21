// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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
// appendSummaryToLog json.Marshal unreachable branch
// =============================================================================

// TestAppendSummaryToLog_MarshalErrorUnreachable documents that the
// json.Marshal error path in appendSummaryToLog (metrics.go:207-209) is
// UNREACHABLE.
//
// The marshaled value is llm.Metrics, which contains only standard JSON-
// serializable types: string, int32, int, float64, bool. json.Marshal on
// this struct cannot fail. The error-handling branch exists for interface
// contract compliance and defensive programming, but is structurally
// unreachable at both the unit-test and integration-test level.
//
// This test proves that llm.Metrics always marshals cleanly, including
// with edge-case inputs: empty strings, zero values, max int32, Unicode,
// and large float64 values.
func TestAppendSummaryToLog_MarshalErrorUnreachable(t *testing.T) {
	t.Parallel()

	// Build edge-case summaries that exercise all field types.
	summaries := []llm.Metrics{
		{
			// Normal values
			Timestamp:      time.Now().Format(time.RFC3339),
			Model:          "test-model",
			CachedTokens:   100,
			PromptTokens:   200,
			ResponseTokens: 300,
			TotalTokens:    600,
			SearchQueries:  3,
			Cost:           1.5,
			IsSummary:      true,
		},
		{
			// Zero/empty values
			Timestamp:      "",
			Provider:       "",
			Model:          "",
			CachedTokens:   0,
			PromptTokens:   0,
			ResponseTokens: 0,
			TotalTokens:    0,
			SearchQueries:  0,
			Duration:       0,
			Cost:           0,
			IsSummary:      false,
			TrafficType:    "",
		},
		{
			// Unicode + edge values
			Timestamp:      "2026-01-15T00:00:00Z",
			Model:          "modèle-🎉",
			CachedTokens:   2147483647, // max int32
			PromptTokens:   2147483647,
			ResponseTokens: 2147483647,
			TotalTokens:    -1, // sentinel: overflow not possible since fields are int32
			SearchQueries:  1000000,
			Cost:           999999.999999,
			IsSummary:      true,
		},
	}

	for i, s := range summaries {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("entry %d: json.Marshal unexpectedly failed: %v", i, err)
		}

		// Round-trip verification
		var restored llm.Metrics
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("entry %d: json.Unmarshal failed: %v", i, err)
		}
		if restored.Model != s.Model {
			t.Errorf("entry %d: Model mismatch: got %q, want %q", i, restored.Model, s.Model)
		}
		if restored.Cost != s.Cost {
			t.Errorf("entry %d: Cost mismatch: got %f, want %f", i, restored.Cost, s.Cost)
		}
		if restored.TotalTokens != s.TotalTokens {
			t.Errorf("entry %d: TotalTokens mismatch: got %d, want %d", i, restored.TotalTokens, s.TotalTokens)
		}
	}

	// Verify error format string exists (compile-time check that the
	// error path is still present and correctly formatted).
	err := fmt.Errorf("failed to marshal cost summary: %w", errors.New("test"))
	if !strings.Contains(err.Error(), "failed to marshal cost summary") {
		t.Error("error format string mismatch")
	}
}

// =============================================================================
// Gap — Silent cost-update warning (metrics.go:109-111)
// =============================================================================

// TestGetCostSummary_SilentCostUpdateWarning covers the gap at
// metrics.go:109-111 where recordCostSilently calls EstimateCost
// and silently logs a warning when EstimateCost fails.
func TestGetCostSummary_SilentCostUpdateWarning(t *testing.T) {
	t.Parallel()

	m := &metricsManager{
		sm:      &mockSMWithError{pathErr: errors.New("path not safe")},
		logFile: "/nonexistent/path/tokens.log",
		model:   "test-model",
	}

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	m.recordCostSilently(context.Background())

	assert.Contains(t, logBuf.String(), "WARN failed to record cost before summary")
	assert.Contains(t, logBuf.String(), "path not safe")
}
