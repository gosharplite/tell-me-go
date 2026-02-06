// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pricing"
)

func TestGetCostSummary_ReportFormat(t *testing.T) {
	tempDir := t.TempDir()

	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history := []SessionCostRecord{
		{
			Date:      "2023-10-27",
			Session:   "session1",
			Model:     "model1",
			TotalCost: 1.5,
			Usage: pricing.UsageStats{
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
			Usage: pricing.UsageStats{
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
			Usage: pricing.UsageStats{
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
	tempDir := t.TempDir()

	globalDir := tempDir
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Create a record at 2023-10-27 08:00:00 CST (UTC+8)
	// CST is 16 hours ahead of PST (UTC-8)
	// 2023-10-27 08:00:00 CST -> 2023-10-26 16:00:00 PST
	ts := time.Date(2023, 10, 27, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))

	history := []SessionCostRecord{
		{
			Date:      "2023-10-27",
			Timestamp: ts,
			Session:   "session-pst",
			Model:     "model1",
			TotalCost: 1.0,
			Usage: pricing.UsageStats{
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
	history = append(history, SessionCostRecord{
		Date:      "2023-10-27",
		Timestamp: ts2,
		Session:   "session-utc",
		Model:     "model1",
		TotalCost: 1.0,
		Usage: pricing.UsageStats{
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
	history = append(history, SessionCostRecord{
		Date:      "2023-10-28", // Local date might be different
		Timestamp: ts3,
		Session:   "session-rollover",
		Model:     "model1",
		TotalCost: 1.0,
		Usage: pricing.UsageStats{
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
