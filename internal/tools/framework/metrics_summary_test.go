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

	"github.com/gosharplite/tell-me-go/internal/pricing"
)

func TestGetCostSummary_ReportFormat(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "summary_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

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

	summary, err := m.getCostSummary(context.Background())
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
