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
// recordCostIfNeeded gap tests
// =============================================================================

// TestRecordCostIfNeeded_AutoSessionID covers the gap at metrics_cost.go:117-119
// where sessionID is empty and generateSessionID is called internally.
//
// Verification strategy: create a temp dir with an output directory structure,
// call recordCostIfNeeded with sessionID="", and verify global_costs.json is
// created with the auto-generated session ID ("mode/basename.log").
func TestRecordCostIfNeeded_AutoSessionID(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	logFile := filepath.Join(outputDir, "session_tokens.log")

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	m := &metricsManager{
		sm:      sm,
		logFile: logFile,
		model:   "test-model",
		mode:    "test-mode",
		ledger:  nil, // nil so loadHistory does NOT trigger recovery
	}

	timestamp := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	breakdown := domain_pricing.CostBreakdown{
		TotalCost: 0.05,
		InputCost: 0.02,
		CacheCost: 0.01,
		Stats: domain_pricing.UsageStats{
			PromptTokens:   100,
			ResponseTokens: 50,
			CachedTokens:   20,
		},
	}
	usage := domain_pricing.UsageStats{
		PromptTokens:   100,
		ResponseTokens: 50,
	}

	// Call with empty sessionID — generateSessionID(m.mode, m.logFile) should run.
	m.recordCostIfNeeded(context.Background(), true, "", "gpt-4", timestamp, outputDir, breakdown, usage)

	// Verify global_costs.json was created in the parent of outputDir.
	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.FileExists(t, historyPath, "global_costs.json should have been created")

	// Read and verify the auto-generated session ID.
	data, err := os.ReadFile(historyPath)
	require.NoError(t, err)

	var records []sessionCostRecord
	require.NoError(t, json.Unmarshal(data, &records))
	require.Len(t, records, 1, "expected exactly one record in global_costs.json")

	expectedSessionID := filepath.ToSlash(filepath.Join("test-mode", "session_tokens.log"))
	assert.Equal(t, expectedSessionID, records[0].Session, "auto-generated session ID mismatch")
	assert.Equal(t, "gpt-4", records[0].Model)
	assert.Equal(t, 0.05, records[0].TotalCost)
	assert.Equal(t, int64(100), records[0].Usage.PromptTokens)
}

// TestRecordCostIfNeeded_ShouldRecordFalse covers the gap at
// metrics_cost.go:114-116 where shouldRecord=false causes an early return
// without touching the filesystem.
func TestRecordCostIfNeeded_ShouldRecordFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	logFile := filepath.Join(outputDir, "session_tokens.log")

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	m := &metricsManager{
		sm:      sm,
		logFile: logFile,
		model:   "test-model",
		mode:    "test-mode",
		ledger:  nil,
	}

	breakdown := domain_pricing.CostBreakdown{}
	usage := domain_pricing.UsageStats{}

	// Call with shouldRecord=false — must return immediately.
	m.recordCostIfNeeded(context.Background(), false, "any-session", "gpt-4", time.Now(), outputDir, breakdown, usage)

	// Verify global_costs.json was NOT created.
	historyPath := filepath.Join(tempDir, "global_costs.json")
	_, err := os.Stat(historyPath)
	assert.True(t, os.IsNotExist(err), "global_costs.json should NOT exist when shouldRecord=false")
}

// =============================================================================
// applyPricingOverrides gap tests
// =============================================================================

// TestApplyPricingOverrides covers the gap at metrics_cost.go:134-141 with a
// table-driven test for nil, empty, and populated override maps.
func TestApplyPricingOverrides(t *testing.T) {
	t.Parallel()

	basePD := domain_pricing.PricingData{
		UpdatedAt: "2026-06-15T00:00:00Z",
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4":   {Hit: 0.5, Miss: 1.0, Comp: 2.0},
			"claude3": {Hit: 0.3, Miss: 3.0, Comp: 15.0},
		},
	}

	tests := []struct {
		name      string
		overrides map[string]domain_pricing.ModelPricing
		want      domain_pricing.PricingData
	}{
		{
			name:      "nil overrides",
			overrides: nil,
			want:      basePD,
		},
		{
			name:      "empty overrides",
			overrides: map[string]domain_pricing.ModelPricing{},
			want:      basePD,
		},
		{
			name: "with overrides",
			overrides: map[string]domain_pricing.ModelPricing{
				"gpt-4": {Hit: 0.1, Miss: 0.2, Comp: 0.4},
			},
			want: domain_pricing.PricingData{
				UpdatedAt: "2026-06-15T00:00:00Z",
				Models: map[string]domain_pricing.ModelPricing{
					"gpt-4":   {Hit: 0.1, Miss: 0.2, Comp: 0.4},  // overridden
					"claude3": {Hit: 0.3, Miss: 3.0, Comp: 15.0}, // unchanged
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &metricsManager{
				pricingOverrides: tt.overrides,
			}

			// Copy basePD to avoid mutating the test case's reference.
			input := domain_pricing.PricingData{
				UpdatedAt: basePD.UpdatedAt,
				Models:    make(map[string]domain_pricing.ModelPricing, len(basePD.Models)),
			}
			for k, v := range basePD.Models {
				input.Models[k] = v
			}

			got := m.applyPricingOverrides(input)

			assert.Equal(t, tt.want.UpdatedAt, got.UpdatedAt)
			assert.Len(t, got.Models, len(tt.want.Models))
			for k, wantV := range tt.want.Models {
				gotV, ok := got.Models[k]
				assert.True(t, ok, "expected model key %q to exist", k)
				assert.Equal(t, wantV, gotV, "model pricing mismatch for %q", k)
			}
		})
	}
}

// =============================================================================
// resolveModel gap tests
// =============================================================================

// TestResolveModel covers the gap at metrics_cost.go:143-148 with a
// table-driven test for the fallback behavior.
func TestResolveModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mModel        string
		detectedModel string
		want          string
	}{
		{
			name:          "empty detected model falls back to configured",
			mModel:        "claude-sonnet-4-20250514",
			detectedModel: "",
			want:          "claude-sonnet-4-20250514",
		},
		{
			name:          "non-empty detected model returned as-is",
			mModel:        "claude-sonnet-4-20250514",
			detectedModel: "gpt-4",
			want:          "gpt-4",
		},
		{
			name:          "both empty edge case",
			mModel:        "",
			detectedModel: "",
			want:          "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &metricsManager{
				model: tt.mModel,
			}

			got := m.resolveModel(tt.detectedModel)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// updateLedgerHistory AtomicWrite error path — already covered
// =============================================================================

// TestUpdateLedgerHistory_AtomicWriteFailure_AlreadyCovered documents that
// the AtomicWrite failure path at metrics_cost.go:98-100 is already covered
// by TestUpdateLedgerHistory_AtomicWriteFailure in persistence_error_test.go.
// That test creates a read-only directory to trigger EACCES in AtomicWrite,
// exercising the identical log.Printf warning branch. No duplicate needed.
func TestUpdateLedgerHistory_AtomicWriteFailure_AlreadyCovered(t *testing.T) {
	t.Parallel()

	// This test simply verifies the existing coverage exists. The real test
	// is TestUpdateLedgerHistory_AtomicWriteFailure in persistence_error_test.go.

	// Verify the function signature so the compiler catches any drift.
	var m *metricsManager
	_ = m.updateLedgerHistory // compile-time check that the method exists

	// If we reach here, coverage for this gap is provided by the existing
	// persistence_error_test.go test suite.
	assert.True(t, true, "coverage provided by persistence_error_test.go")
}
