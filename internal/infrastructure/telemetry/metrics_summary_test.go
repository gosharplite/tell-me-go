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

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestGetCostSummary_ReportFormat(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history := []sessionCostRecord{
		{
			Date:      "2023-10-27",
			Session:   "session1",
			Model:     "model1",
			TotalCost: 1.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   1000,
				CachedTokens:   200,
				ResponseTokens: 300,
				ThinkingTokens: 100,
			},
		},
		{
			Date:      "2023-10-27",
			Session:   "session2",
			Model:     "model1",
			TotalCost: 0.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   500,
				CachedTokens:   0,
				ResponseTokens: 100,
				ThinkingTokens: 0,
			},
		},
		{
			Date:      "2023-10-26",
			Session:   "session3",
			Model:     "model1",
			TotalCost: 2.0,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   2000,
				CachedTokens:   500,
				ResponseTokens: 400,
				ThinkingTokens: 200,
			},
		},
	}

	data, _ := json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	m := &metricsManager{
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	summary, err := m.getCostSummary(context.Background(), costSummaryArgs{Billing: false})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}

	// Verify headers
	if !strings.Contains(summary, "| Date | Miss | Hit | Other | Eff % | Total Cost (USD) |") {
		t.Errorf("summary missing expected header: %s", summary)
	}

	// Verify 2023-10-27 aggregates
	// Session 1: M=800, H=200, O=400, Cost=1.5
	// Session 2: M=500, H=0, O=100, Cost=0.5
	// Total: M=1300, H=200, O=500, Cost=2.0
	// Eff: 200 / 1500 = 13.3%
	expected27 := "| 2023-10-27 | 1300 | 200 | 500 | 13.3% | $2.0000 |"
	if !strings.Contains(summary, expected27) {
		t.Errorf("summary missing expected row for 2023-10-27: %s", summary)
	}

	// Verify 2023-10-26 aggregates
	// Session 3: M=1500, H=500, O=600, Cost=2.0
	// Eff: 500 / 2000 = 25.0%
	expected26 := "| 2023-10-26 | 1500 | 500 | 600 | 25.0% | $2.0000 |"
	if !strings.Contains(summary, expected26) {
		t.Errorf("summary missing expected row for 2023-10-26: %s", summary)
	}

	// Verify Grand Total
	// Total M: 1300 + 1500 = 2800
	// Total H: 200 + 500 = 700
	// Total O: 500 + 600 = 1100
	// Total Cost: 2.0 + 2.0 = 4.0
	// Total Eff: 700 / 3500 = 20.0%
	expectedGrand := "| **Grand Total** | **2800** | **700** | **1100** | **20.0%** | **$4.0000** |"
	if !strings.Contains(summary, expectedGrand) {
		t.Errorf("summary missing expected grand total: %s", summary)
	}
}

func TestGetCostSummary_GoogleBilling(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Create a record at 2023-10-27 08:00:00 CST (UTC+8)
	// CST is 16 hours ahead of PST (UTC-8)
	// 2023-10-27 08:00:00 CST -> 2023-10-26 16:00:00 PST
	ts := time.Date(2023, 10, 27, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))

	history := []sessionCostRecord{
		{
			Date:      "2023-10-27",
			Timestamp: ts,
			Session:   "session-pst",
			Model:     "model1",
			TotalCost: 1.0,
			Usage: domain_pricing.UsageStats{
				PromptTokens: 1000,
			},
		},
	}

	data, _ := json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	m := &metricsManager{
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	// 1. Regular summary (now defaults to Local time)
	summary, err := m.getCostSummary(context.Background(), costSummaryArgs{Billing: false})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}
	// ts is 2023-10-27 08:00:00 CST (UTC+8).
	// We expect the date to match whatever ts.Local() produces.
	expectedDate := ts.Local().Format("2006-01-02")
	if !strings.Contains(summary, expectedDate) {
		t.Errorf("Expected %s in regular local-based summary, got:\n%s", expectedDate, summary)
	}

	// 2. Google Billing summary (offset -8)
	summary, err = m.getCostSummary(context.Background(), costSummaryArgs{Billing: true})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}
	// 2023-10-27 08:00:00 CST (UTC+8) -> 2023-10-26 16:00:00 PST (UTC-8)
	if !strings.Contains(summary, "2023-10-26") {
		t.Errorf("Expected 2023-10-26 in billing summary, got:\n%s", summary)
	}
	// If local time is NOT UTC-8, then 2023-10-27 should not be here (it was shifted to 26th)
	// But wait, if local time is UTC-8, then ts.Local() is also 2023-10-26.
	// The point is that Billing: true forced it to 26th.

	// 3. New test case for UTC timestamp that was previously buggy
	// Timestamp: 2023-10-27 10:00:00 UTC
	// In UTC-8, this is 2023-10-27 02:00:00.
	ts2 := time.Date(2023, 10, 27, 10, 0, 0, 0, time.UTC)
	history = append(history, sessionCostRecord{
		Date:      "2023-10-27",
		Timestamp: ts2,
		Session:   "session-utc",
		Model:     "model1",
		TotalCost: 1.0,
		Usage: domain_pricing.UsageStats{
			PromptTokens: 1000,
		},
	})
	data, _ = json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	summary, err = m.getCostSummary(context.Background(), costSummaryArgs{Billing: true})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}
	// The new record should be 2023-10-27 in UTC-8
	if !strings.Contains(summary, "2023-10-27") {
		t.Errorf("Expected 2023-10-27 in billing summary for 10:00 UTC timestamp, got:\n%s", summary)
	}
	// 4. Test UTC rollover
	// Timestamp: 2023-10-27 23:59:59 UTC
	ts3 := time.Date(2023, 10, 27, 23, 59, 59, 0, time.UTC)
	history = append(history, sessionCostRecord{
		Date:      "2023-10-28", // Local date might be different
		Timestamp: ts3,
		Session:   "session-rollover",
		Model:     "model1",
		TotalCost: 1.0,
		Usage: domain_pricing.UsageStats{
			PromptTokens: 1000,
		},
	})
	data, _ = json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	summary, err = m.getCostSummary(context.Background(), costSummaryArgs{Billing: false})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}
	// It should be attributed to the date in local time.
	expectedDate3 := ts3.Local().Format("2006-01-02")
	if !strings.Contains(summary, expectedDate3) {
		t.Errorf("Expected %s in regular summary for 23:59:59 UTC timestamp, got:\n%s", expectedDate3, summary)
	}
}

func TestGetCostSummary_Filters(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Use Billing: true (UTC-8) for predictable results regardless of host TZ
	tz := time.FixedZone("UTC-8", -8*3600)

	// 2023-10-25 10:30 UTC-8
	ts1 := time.Date(2023, 10, 25, 10, 30, 0, 0, tz)
	// 2023-10-25 11:15 UTC-8
	ts2 := time.Date(2023, 10, 25, 11, 15, 0, 0, tz)
	// 2023-10-26 23:59 UTC-8
	ts3 := time.Date(2023, 10, 26, 23, 59, 0, 0, tz)
	// 2023-10-27 00:01 UTC-8
	ts4 := time.Date(2023, 10, 27, 0, 1, 0, 0, tz)

	history := []sessionCostRecord{
		{Date: "2023-10-25", Timestamp: ts1, Session: "s1", TotalCost: 0.1, Usage: domain_pricing.UsageStats{PromptTokens: 100}},
		{Date: "2023-10-25", Timestamp: ts2, Session: "s2", TotalCost: 0.2, Usage: domain_pricing.UsageStats{PromptTokens: 200}},
		{Date: "2023-10-26", Timestamp: ts3, Session: "s3", TotalCost: 0.3, Usage: domain_pricing.UsageStats{PromptTokens: 300}},
		{Date: "2023-10-27", Timestamp: ts4, Session: "s4", TotalCost: 0.4, Usage: domain_pricing.UsageStats{PromptTokens: 400}},
	}
	data, _ := json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	m := &metricsManager{
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	tests := []struct {
		name      string
		args      costSummaryArgs
		wantRows  []string
		wantTotal string
		wantErr   bool
	}{
		{
			name: "StartDate only",
			args: costSummaryArgs{StartDate: "2023-10-26", Billing: true},
			wantRows: []string{
				"2023-10-27",
				"2023-10-26",
			},
			wantTotal: "$0.7000",
		},
		{
			name: "EndDate only",
			args: costSummaryArgs{EndDate: "2023-10-25", Billing: true},
			wantRows: []string{
				"2023-10-25",
			},
			wantTotal: "$0.3000",
		},
		{
			name: "Range filter",
			args: costSummaryArgs{StartDate: "2023-10-25", EndDate: "2023-10-26", Billing: true},
			wantRows: []string{
				"2023-10-26",
				"2023-10-25",
			},
			wantTotal: "$0.6000",
		},
		{
			name: "Interval hour",
			args: costSummaryArgs{StartDate: "2023-10-25", EndDate: "2023-10-25", Interval: "hour", Billing: true},
			wantRows: []string{
				"2023-10-25 11:00",
				"2023-10-25 10:00",
			},
			wantTotal: "$0.3000",
		},
		{
			name: "Inclusive EndDate boundary",
			args: costSummaryArgs{StartDate: "2023-10-27", EndDate: "2023-10-27", Billing: true},
			wantRows: []string{
				"2023-10-27",
			},
			wantTotal: "$0.4000",
		},
		{
			name:    "Invalid StartDate",
			args:    costSummaryArgs{StartDate: "invalid"},
			wantErr: true,
		},
		{
			name:    "Invalid EndDate",
			args:    costSummaryArgs{EndDate: "invalid"},
			wantErr: true,
		},
		{
			name:    "Invalid Interval",
			args:    costSummaryArgs{Interval: "month"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := m.getCostSummary(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCostSummary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			for _, row := range tt.wantRows {
				if !strings.Contains(res, row) {
					t.Errorf("expected row %q not found in summary:\n%s", row, res)
				}
			}
			if !strings.Contains(res, tt.wantTotal) {
				t.Errorf("expected grand total %q not found in summary:\n%s", tt.wantTotal, res)
			}
		})
	}
}

func TestGetCostSummary_MalformedRecords(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history := []sessionCostRecord{
		{
			Date:      "2023-10-27",
			Timestamp: time.Date(2023, 10, 27, 10, 0, 0, 0, time.UTC),
			Session:   "valid",
			TotalCost: 1.0,
			Usage:     domain_pricing.UsageStats{PromptTokens: 1000},
		},
		{
			Date:      "invalid-date",
			Session:   "malformed",
			TotalCost: 2.0,
			Usage:     domain_pricing.UsageStats{PromptTokens: 2000},
		},
		{
			// Missing both Date and Timestamp
			Session:   "missing",
			TotalCost: 3.0,
			Usage:     domain_pricing.UsageStats{PromptTokens: 3000},
		},
	}

	data, _ := json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	m := &metricsManager{
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	summary, err := m.getCostSummary(context.Background(), costSummaryArgs{Billing: false})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}

	// Should only contain the valid record
	if !strings.Contains(summary, "2023-10-27") {
		t.Errorf("Expected valid record to be present")
	}
	if strings.Contains(summary, "$6.0000") {
		t.Errorf("Malformed records should have been skipped, but grand total is $6.0000")
	}
	if !strings.Contains(summary, "$1.0000") {
		t.Errorf("Grand total should be $1.0000, got summary:\n%s", summary)
	}
}

func TestGetCostSummary_GroupByModel(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history := []sessionCostRecord{
		{
			Date:      "2023-10-27",
			Session:   "session1",
			Model:     "gemini-1.5-pro",
			TotalCost: 1.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   1000,
				CachedTokens:   200,
				ResponseTokens: 300,
			},
		},
		{
			Date:      "2023-10-27",
			Session:   "session2",
			Model:     "gpt-4o",
			TotalCost: 0.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   500,
				CachedTokens:   0,
				ResponseTokens: 100,
			},
		},
		{
			Date:      "2023-10-26",
			Session:   "session3",
			Model:     "gemini-1.5-pro",
			TotalCost: 2.0,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   2000,
				CachedTokens:   500,
				ResponseTokens: 400,
			},
		},
	}

	data, _ := json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	m := &metricsManager{
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	summary, err := m.getCostSummary(context.Background(), costSummaryArgs{GroupBy: "model"})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}

	// Verify headers
	if !strings.Contains(summary, "### AI Usage Cost Summary (by Model)") {
		t.Errorf("summary missing expected title: %s", summary)
	}
	if !strings.Contains(summary, "| Model | Miss | Hit | Other | Eff % | Total Cost (USD) |") {
		t.Errorf("summary missing expected header: %s", summary)
	}

	// Verify gemini-1.5-pro aggregates
	// Session 1: M=800, H=200, O=300, Cost=1.5
	// Session 3: M=1500, H=500, O=400, Cost=2.0
	// Total: M=2300, H=700, O=700, Cost=3.5
	// Eff: 700 / 3000 = 23.3%
	expectedGemini := "| gemini-1.5-pro | 2300 | 700 | 700 | 23.3% | $3.5000 |"
	if !strings.Contains(summary, expectedGemini) {
		t.Errorf("summary missing expected row for gemini-1.5-pro: %s", summary)
	}

	// Verify gpt-4o aggregates
	// Session 2: M=500, H=0, O=100, Cost=0.5
	// Total: M=500, H=0, O=100, Cost=0.5
	// Eff: 0%
	expectedGPT := "| gpt-4o | 500 | 0 | 100 | 0.0% | $0.5000 |"
	if !strings.Contains(summary, expectedGPT) {
		t.Errorf("summary missing expected row for gpt-4o: %s", summary)
	}

	// Verify Grand Total
	// Total Cost: 3.5 + 0.5 = 4.0
	expectedGrand := "| **Grand Total** | **2800** | **700** | **800** | **20.0%** | **$4.0000** |"
	if !strings.Contains(summary, expectedGrand) {
		t.Errorf("summary missing expected grand total: %s", summary)
	}
}

func TestGetCostSummary_GroupByDateModel(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history := []sessionCostRecord{
		{
			Date:      "2023-10-27",
			Session:   "session1",
			Model:     "gemini-1.5-pro",
			TotalCost: 1.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   1000,
				CachedTokens:   200,
				ResponseTokens: 300,
			},
		},
		{
			Date:      "2023-10-27",
			Session:   "session2",
			Model:     "gpt-4o",
			TotalCost: 0.5,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   500,
				CachedTokens:   0,
				ResponseTokens: 100,
			},
		},
		{
			Date:      "2023-10-26",
			Session:   "session3",
			Model:     "gemini-1.5-pro",
			TotalCost: 2.0,
			Usage: domain_pricing.UsageStats{
				PromptTokens:   2000,
				CachedTokens:   500,
				ResponseTokens: 400,
			},
		},
	}

	data, _ := json.Marshal(history)
	_ = os.WriteFile(historyPath, data, 0644)

	m := &metricsManager{
		logFile: filepath.Join(tempDir, "mode", "tokens.log"),
	}

	summary, err := m.getCostSummary(context.Background(), costSummaryArgs{GroupBy: "date,model"})
	if err != nil {
		t.Fatalf("getCostSummary failed: %v", err)
	}

	// Verify title and headers
	if !strings.Contains(summary, "### AI Usage Cost Summary (by Date and Model)") {
		t.Errorf("summary missing expected title: %s", summary)
	}
	if !strings.Contains(summary, "| Date | Model | Miss | Hit | Other | Eff % | Total Cost (USD) |") {
		t.Errorf("summary missing expected header: %s", summary)
	}

	// Verify rows
	// 2023-10-27 | gemini-1.5-pro: M=800, H=200, O=300, Cost=1.5
	expected1 := "| 2023-10-27 | gemini-1.5-pro | 800 | 200 | 300 | 20.0% | $1.5000 |"
	if !strings.Contains(summary, expected1) {
		t.Errorf("summary missing expected row: %s", expected1)
	}

	// 2023-10-27 | gpt-4o: M=500, H=0, O=100, Cost=0.5
	expected2 := "| 2023-10-27 | gpt-4o | 500 | 0 | 100 | 0.0% | $0.5000 |"
	if !strings.Contains(summary, expected2) {
		t.Errorf("summary missing expected row: %s", expected2)
	}

	// 2023-10-26 | gemini-1.5-pro: M=1500, H=500, O=400, Cost=2.0
	expected3 := "| 2023-10-26 | gemini-1.5-pro | 1500 | 500 | 400 | 25.0% | $2.0000 |"
	if !strings.Contains(summary, expected3) {
		t.Errorf("summary missing expected row: %s", expected3)
	}
}

// ---------------------------------------------------------------------------
// ensureLedgerReady extended tests (Phase 6)
// ---------------------------------------------------------------------------

func TestEnsureLedgerReady_RecoveryInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	// Need an outputDir structure so ensureLedgerReady can compute paths.
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(tempDir, "global_costs.json")
	// Pre-create a valid ledger so we don't hit the "missing" branch first.
	if err := os.WriteFile(historyPath, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate recovery in progress.
	recoveryInProgress.Store(historyPath, true)
	t.Cleanup(func() { recoveryInProgress.Delete(historyPath) })

	m := &metricsManager{
		logFile: filepath.Join(outputDir, "tokens.log"),
	}
	_, status, err := m.ensureLedgerReady(context.Background(), historyPath, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == "" {
		t.Fatal("expected non-empty status when recovery is in progress")
	}
	if !strings.Contains(status, "recovery is currently in progress") {
		t.Errorf("unexpected status: %s", status)
	}
}

func TestEnsureLedgerReady_ReadFileError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: os.Chmod(0000) does not prevent reading for the owner.")
	}
	// NOT parallel — chmod cleanup sensitive.
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(tempDir, "global_costs.json")
	// Create file with mode 0000 so os.ReadFile fails with permission denied.
	if err := os.WriteFile(historyPath, []byte("valid json"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(historyPath, 0644) })

	m := &metricsManager{
		logFile: filepath.Join(outputDir, "tokens.log"),
	}
	_, status, err := m.ensureLedgerReady(context.Background(), historyPath, tempDir)

	// Should not error — the function catches ReadFile errors and returns a status.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == "" {
		t.Fatal("expected non-empty status for unreadable ledger file")
	}
	if !strings.Contains(status, "No cost history found") {
		t.Errorf("unexpected status: %s", status)
	}
}
