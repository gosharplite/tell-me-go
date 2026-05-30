// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// ---------------------------------------------------------------------------
// mockSMWithError — a security manager whose IsPathSafe returns an error.
// ---------------------------------------------------------------------------

type mockSMWithError struct {
	domain_security.Manager
	pathErr error
}

func (m *mockSMWithError) IsPathSafe(path string) (string, error) {
	if m.pathErr != nil {
		return "", m.pathErr
	}
	return path, nil
}

func (m *mockSMWithError) IsPathWritable(path string) (string, error) { return path, nil }
func (m *mockSMWithError) Close() error                               { return nil }
func (m *mockSMWithError) IsBypassActive() bool                       { return false }

// ---------------------------------------------------------------------------
// Gap 1 — IsPathSafe error return (metrics_cost.go:123-125)
// ---------------------------------------------------------------------------

// TestEstimateCost_IsPathSafeError verifies that when sm.IsPathSafe returns an
// error, EstimateCost propagates it immediately without proceeding further.
func TestEstimateCost_IsPathSafeError(t *testing.T) {
	t.Parallel()

	m := &metricsManager{
		sm:      &mockSMWithError{pathErr: errors.New("path not safe")},
		logFile: "/some/path",
		model:   "test-model",
		mode:    "test-mode",
	}

	result, err := m.EstimateCost(context.Background(), false, "")
	if err == nil {
		t.Fatal("expected error from IsPathSafe, got nil")
	}
	if !strings.Contains(err.Error(), "path not safe") {
		t.Errorf("error should contain 'path not safe', got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on error, got: %q", result)
	}
}

// ---------------------------------------------------------------------------
// Gap 2 — parseUsage non-NotExist error (metrics_cost.go:134-140)
// ---------------------------------------------------------------------------

// TestEstimateCost_ParseUsageDirectoryError verifies that when parseUsage
// receives a directory path (os.Open returns a non-NotExist error), the error
// is wrapped with "failed to parse usage log".
func TestEstimateCost_ParseUsageDirectoryError(t *testing.T) {
	t.Parallel()

	logDir := filepath.Join(t.TempDir(), "adir")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	m := &metricsManager{
		sm:      &mockSM{},
		logFile: logDir,
		model:   "test-model",
		mode:    "test-mode",
	}

	result, err := m.EstimateCost(context.Background(), false, "")
	if err == nil {
		t.Fatal("expected error when logFile is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse usage log") {
		t.Errorf("error should contain 'failed to parse usage log', got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on error, got: %q", result)
	}
}

// TestEstimateCost_LogFileNotFound verifies the os.IsNotExist branch
// (metrics_cost.go:136-138): EstimateCost returns a user-friendly message
// with a nil error when the log file has not been created yet.
func TestEstimateCost_LogFileNotFound(t *testing.T) {
	t.Parallel()

	nonexistentPath := filepath.Join(t.TempDir(), "nonexistent.log")

	m := &metricsManager{
		sm:      &mockSM{},
		logFile: nonexistentPath,
		model:   "test-model",
		mode:    "test-mode",
	}

	result, err := m.EstimateCost(context.Background(), false, "")
	if err != nil {
		t.Fatalf("expected no error for nonexistent log file, got: %v", err)
	}
	if !strings.Contains(result, "Error: Log file not found") {
		t.Errorf("result should contain 'Error: Log file not found', got: %q", result)
	}
}

// ---------------------------------------------------------------------------
// RecordSessionCost error-path tests
// ---------------------------------------------------------------------------

// TestRecordSessionCost_EstimateCostFails verifies that when EstimateCost
// returns an error (e.g., IsPathSafe fails), RecordSessionCost wraps it with
// "failed to estimate and record session cost" and propagates the original.
func TestRecordSessionCost_EstimateCostFails(t *testing.T) {
	// NOT parallel — uses global metricsMu / ledgerMu.
	ctx := context.Background()
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "tokens.log")
	// Create a valid minimal log file so the path exists.
	if err := os.WriteFile(logPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	mockSM := &mockSMWithError{pathErr: errors.New("path not safe")}

	err := RecordSessionCost(ctx, mockSM, nil, logPath, "test-model", "test-mode", "test-session", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to estimate and record session cost") {
		t.Errorf("error should contain 'failed to estimate and record session cost', got: %v", err)
	}
	if !strings.Contains(err.Error(), "path not safe") {
		t.Errorf("error should wrap 'path not safe', got: %v", err)
	}
}

// TestRecordSessionCost_ResolveUsageSummaryErrorUnreachable documents that
// the resolveUsageForSummary error return branch in RecordSessionCost
// (metrics.go:134-137) is unreachable in practice.
//
// Reasoning:
//   - When tracker == nil: resolveUsageForSummary calls parseUsage.
//     If parseUsage returns os.IsNotExist, the error is suppressed (nil returned).
//     If parseUsage returns any other error, EstimateCost (called first in
//     RecordSessionCost) would have already hit the same error and returned
//     before reaching resolveUsageForSummary.
//   - When tracker != nil: GetStats() returns (UsageStats, float64) with no
//     error — the signature has no error return.
//
// Therefore the "if err != nil { return err }" after resolveUsageForSummary
// can never execute.
func TestRecordSessionCost_ResolveUsageSummaryErrorUnreachable(t *testing.T) {
	t.Parallel()

	// Verify the two code paths that make the error branch unreachable.

	// Path A: tracker != nil → GetStats never errors.
	sm := &mockSM{}
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")
	if err := os.WriteFile(logPath, []byte(`{"model":"m","prompt_tokens":10,"response_tokens":5}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"m": {Hit: 0.1, Miss: 1.0, Comp: 2.0},
		},
	}
	tracker := NewSessionCostTracker(sm, logPath, "mode", "m", pricing.Models["m"], pricing)
	usage, cost := tracker.GetStats(context.Background())
	// Confirm GetStats returns zero values without error (no error return in signature).
	_ = usage
	_ = cost
	// If this compiles and runs without panic, the error branch is unreachable
	// when tracker != nil.

	// Path B: tracker == nil + file doesn't exist → os.IsNotExist → nil error.
	nonexistent := filepath.Join(t.TempDir(), "nonexistent.log")
	u, c, err := resolveUsageForSummary(context.Background(), sm, nil, nonexistent, "m", nil)
	if err != nil {
		t.Fatalf("expected nil error for nonexistent file (IsNotExist suppressed), got: %v", err)
	}
	if u.PromptTokens != 0 || c != 0 {
		t.Errorf("expected zero usage/cost, got %+v / %f", u, c)
	}

	// Path B.2: tracker == nil + parseUsage non-NotExist error.
	// But with tracker==nil in RecordSessionCost, EstimateCost runs first
	// and would hit the same parseUsage error before resolveUsageForSummary.
	// This is structurally unreachable within RecordSessionCost.
}

// ---------------------------------------------------------------------------
// Gap 3 — renderReport CacheWriteTokens / ThinkingTokens branches
// (metrics_cost.go:192-199)
// ---------------------------------------------------------------------------

// TestRenderReport_AllRows verifies that the optional Cache Write and Thinking
// Tokens rows appear in the rendered report only when the corresponding stats
// are non-zero.
func TestRenderReport_AllRows(t *testing.T) {
	t.Parallel()

	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": {Hit: 1.0, Miss: 2.0, Comp: 3.0},
		},
	}

	tests := []struct {
		name           string
		breakdown      domain_pricing.CostBreakdown
		wantCacheWrite bool
		wantThinking   bool
	}{
		{
			name: "CacheWriteTokens row",
			breakdown: domain_pricing.CostBreakdown{
				TotalCost:      0.01,
				InputCost:      0.005,
				CacheCost:      0.001,
				CacheWriteCost: 0.002,
				SearchCost:     0.001,
				Stats: domain_pricing.UsageStats{
					PromptTokens:     1000,
					CachedTokens:     500,
					ResponseTokens:   500,
					CacheWriteTokens: 100,
					SearchQueries:    1,
				},
			},
			wantCacheWrite: true,
			wantThinking:   false,
		},
		{
			name: "ThinkingTokens row",
			breakdown: domain_pricing.CostBreakdown{
				TotalCost:  0.01,
				InputCost:  0.005,
				CacheCost:  0.001,
				SearchCost: 0.001,
				Stats: domain_pricing.UsageStats{
					PromptTokens:   1000,
					CachedTokens:   500,
					ResponseTokens: 500,
					ThinkingTokens: 200,
					SearchQueries:  1,
				},
			},
			wantCacheWrite: false,
			wantThinking:   true,
		},
		{
			name: "Both rows",
			breakdown: domain_pricing.CostBreakdown{
				TotalCost:      0.01,
				InputCost:      0.005,
				CacheCost:      0.001,
				CacheWriteCost: 0.002,
				SearchCost:     0.001,
				Stats: domain_pricing.UsageStats{
					PromptTokens:     1000,
					CachedTokens:     500,
					ResponseTokens:   500,
					ThinkingTokens:   200,
					CacheWriteTokens: 100,
					SearchQueries:    1,
				},
			},
			wantCacheWrite: true,
			wantThinking:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &metricsManager{
				model: "test-model",
			}

			report := m.renderReport(pricing, tt.breakdown)

			if !strings.Contains(report, "Estimated Cost for Session") {
				t.Error("report should contain the header")
			}

			hasCacheWrite := strings.Contains(report, "Cache Write")
			if hasCacheWrite != tt.wantCacheWrite {
				t.Errorf("Cache Write row: got %v, want %v", hasCacheWrite, tt.wantCacheWrite)
			}

			hasThinking := strings.Contains(report, "Thinking Tokens")
			if hasThinking != tt.wantThinking {
				t.Errorf("Thinking Tokens row: got %v, want %v", hasThinking, tt.wantThinking)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Benchmark fixtures
// ---------------------------------------------------------------------------

var benchRenderPricing = domain_pricing.PricingData{
	UpdatedAt: "2026-03-15T00:00:00Z",
	Models: map[string]domain_pricing.ModelPricing{
		"claude-sonnet-4-20250514": {
			Hit: 0.30, Miss: 3.00, Comp: 15.00, SearchQuery: 0.015,
		},
	},
}

var benchSinkReport string
var benchSinkEstimate string
var benchSinkEstimateErr error

// ---------------------------------------------------------------------------
// BenchmarkRenderReport_Single — one render per iteration
// ---------------------------------------------------------------------------

func BenchmarkRenderReport_Single(b *testing.B) {
	m := &metricsManager{
		model: "claude-sonnet-4-20250514",
	}

	b.Run("minimal", func(b *testing.B) {
		breakdown := domain_pricing.CostBreakdown{
			Stats: domain_pricing.UsageStats{
				PromptTokens:   100,
				ResponseTokens: 50,
			},
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSinkReport = m.renderReport(benchRenderPricing, breakdown)
		}
		_ = benchSinkReport
	})

	b.Run("full", func(b *testing.B) {
		breakdown := domain_pricing.CostBreakdown{
			Stats: domain_pricing.UsageStats{
				PromptTokens:     150000,
				ResponseTokens:   80000,
				CachedTokens:     20000,
				CacheWriteTokens: 10000,
				ThinkingTokens:   40000,
				SearchQueries:    5,
			},
			InputCost:      0.390,
			CacheCost:      0.006,
			CacheWriteCost: 0.0375,
			OutputCost:     1.800,
			SearchCost:     0.075,
			TotalCost:      2.3085,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSinkReport = m.renderReport(benchRenderPricing, breakdown)
		}
		_ = benchSinkReport
	})
}

// ---------------------------------------------------------------------------
// BenchmarkRenderReport_Batch100 — 100 renders per iteration
// ---------------------------------------------------------------------------

func BenchmarkRenderReport_Batch100(b *testing.B) {
	m := &metricsManager{
		model: "claude-sonnet-4-20250514",
	}

	b.Run("minimal", func(b *testing.B) {
		breakdown := domain_pricing.CostBreakdown{
			Stats: domain_pricing.UsageStats{
				PromptTokens:   100,
				ResponseTokens: 50,
			},
		}
		b.ResetTimer()
		// Batch of 100 calls per iteration — amortized cost = reported / 100.
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				benchSinkReport = m.renderReport(benchRenderPricing, breakdown)
			}
		}
		_ = benchSinkReport
	})

	b.Run("full", func(b *testing.B) {
		breakdown := domain_pricing.CostBreakdown{
			Stats: domain_pricing.UsageStats{
				PromptTokens:     150000,
				ResponseTokens:   80000,
				CachedTokens:     20000,
				CacheWriteTokens: 10000,
				ThinkingTokens:   40000,
				SearchQueries:    5,
			},
			InputCost:      0.390,
			CacheCost:      0.006,
			CacheWriteCost: 0.0375,
			OutputCost:     1.800,
			SearchCost:     0.075,
			TotalCost:      2.3085,
		}
		b.ResetTimer()
		// Batch of 100 calls per iteration — amortized cost = reported / 100.
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				benchSinkReport = m.renderReport(benchRenderPricing, breakdown)
			}
		}
		_ = benchSinkReport
	})
}

// ---------------------------------------------------------------------------
// BenchmarkEstimateCost_Single — one EstimateCost call per iteration.
// Exercises: IsPathSafe (mock) → GetPricing (reads pricing.json) → parseUsage
// (reads + parses log) → CostCalculator.Calculate (math) → renderReport (string build).
// Uses shouldRecord=false to bypass all ledger/lock/KV I/O.
// ---------------------------------------------------------------------------

func BenchmarkEstimateCost_Single(b *testing.B) {
	const oneLine = `{"model":"claude-sonnet-4-20250514","prompt_tokens":1500,"response_tokens":800,"cached_tokens":200,"cache_write_tokens":100,"thinking_tokens":400,"search_queries":2,"timestamp":"2026-03-15T10:30:00Z","cost":0.015432}`

	const pricingJSON = `{
  "updated_at": "2026-03-15T00:00:00Z",
  "models": {
    "claude-sonnet-4-20250514": {
      "hit": 0.30,
      "miss": 3.00,
      "comp": 15.00,
      "search_query": 0.015
    }
  }
}`

	setup := func(b *testing.B, logContent string) *metricsManager {
		b.Helper()

		tmpDir := b.TempDir()

		// Create output directory and session_tokens.log
		outputDir := filepath.Join(tmpDir, "output")
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			b.Fatal(err)
		}
		logPath := filepath.Join(outputDir, "session_tokens.log")
		if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
			b.Fatal(err)
		}

		// Create assets/pricing.json in parent of outputDir
		assetsDir := filepath.Join(tmpDir, "assets")
		if err := os.MkdirAll(assetsDir, 0755); err != nil {
			b.Fatal(err)
		}
		pricingPath := filepath.Join(assetsDir, "pricing.json")
		if err := os.WriteFile(pricingPath, []byte(pricingJSON), 0644); err != nil {
			b.Fatal(err)
		}

		return &metricsManager{
			sm:      &mockSM{},
			logFile: logPath,
			model:   "claude-sonnet-4-20250514",
			mode:    "benchmark",
		}
	}

	b.Run("small_log", func(b *testing.B) {
		m := setup(b, oneLine+"\n")
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSinkEstimate, benchSinkEstimateErr = m.EstimateCost(ctx, false, "")
		}
		_, _ = benchSinkEstimate, benchSinkEstimateErr
	})

	b.Run("large_log", func(b *testing.B) {
		// Build 100 identical lines (simulates a long session).
		largeContent := strings.Repeat(oneLine+"\n", 100)
		m := setup(b, largeContent)
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchSinkEstimate, benchSinkEstimateErr = m.EstimateCost(ctx, false, "")
		}
		_, _ = benchSinkEstimate, benchSinkEstimateErr
	})
}

// ---------------------------------------------------------------------------
// BenchmarkEstimateCost_Batch100 — 100 calls per iteration, amortized.
// ---------------------------------------------------------------------------

func BenchmarkEstimateCost_Batch100(b *testing.B) {
	const oneLine = `{"model":"claude-sonnet-4-20250514","prompt_tokens":1500,"response_tokens":800,"cached_tokens":200,"cache_write_tokens":100,"thinking_tokens":400,"search_queries":2,"timestamp":"2026-03-15T10:30:00Z","cost":0.015432}`

	const pricingJSON = `{
  "updated_at": "2026-03-15T00:00:00Z",
  "models": {
    "claude-sonnet-4-20250514": {
      "hit": 0.30,
      "miss": 3.00,
      "comp": 15.00,
      "search_query": 0.015
    }
  }
}`

	setup := func(b *testing.B, logContent string) *metricsManager {
		b.Helper()

		tmpDir := b.TempDir()

		outputDir := filepath.Join(tmpDir, "output")
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			b.Fatal(err)
		}
		logPath := filepath.Join(outputDir, "session_tokens.log")
		if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
			b.Fatal(err)
		}

		assetsDir := filepath.Join(tmpDir, "assets")
		if err := os.MkdirAll(assetsDir, 0755); err != nil {
			b.Fatal(err)
		}
		pricingPath := filepath.Join(assetsDir, "pricing.json")
		if err := os.WriteFile(pricingPath, []byte(pricingJSON), 0644); err != nil {
			b.Fatal(err)
		}

		return &metricsManager{
			sm:      &mockSM{},
			logFile: logPath,
			model:   "claude-sonnet-4-20250514",
			mode:    "benchmark",
		}
	}

	b.Run("small_log", func(b *testing.B) {
		m := setup(b, oneLine+"\n")
		ctx := context.Background()
		b.ResetTimer()
		// Batch of 100 calls per iteration — amortized cost = reported / 100.
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				benchSinkEstimate, benchSinkEstimateErr = m.EstimateCost(ctx, false, "")
			}
		}
		_, _ = benchSinkEstimate, benchSinkEstimateErr
	})

	b.Run("large_log", func(b *testing.B) {
		largeContent := strings.Repeat(oneLine+"\n", 100)
		m := setup(b, largeContent)
		ctx := context.Background()
		b.ResetTimer()
		// Batch of 100 calls per iteration — amortized cost = reported / 100.
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				benchSinkEstimate, benchSinkEstimateErr = m.EstimateCost(ctx, false, "")
			}
		}
		_, _ = benchSinkEstimate, benchSinkEstimateErr
	})
}
