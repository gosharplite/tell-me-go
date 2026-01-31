// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordCost(t *testing.T) {
	// 1. Setup Temp Dir
	tmpDir, err := os.MkdirTemp("", "metrics_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	modeDir := filepath.Join(tmpDir, "testmode")
	os.MkdirAll(modeDir, 0755)

	logFile := filepath.Join(modeDir, "tokens.log")
	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)

	m := &metricsManager{
		sm:      sm,
		logFile: logFile,
		model:   "gemini-test",
		mode:    "testmode",
	}

	// Helper to read history
	readHistory := func() []SessionCostRecord {
		path := filepath.Join(tmpDir, "global_costs.json")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return []SessionCostRecord{}
		}
		if err != nil {
			t.Fatalf("Failed to read history file: %v", err)
		}
		var history []SessionCostRecord
		if len(data) > 0 {
			if err := json.Unmarshal(data, &history); err != nil {
				t.Fatalf("Failed to unmarshal history: %v", err)
			}
		}
		return history
	}

	// Test 1: Record new session
	t.Run("RecordNew", func(t *testing.T) {
		record := SessionCostRecord{
			Date:      "2026-01-01",
			Session:   "session-1.log",
			Model:     "gemini-test",
			TotalCost: 0.1234,
		}
		m.recordCost(context.Background(), modeDir, "testmode", record)

		history := readHistory()
		if len(history) != 1 {
			t.Fatalf("Expected 1 record, got %d", len(history))
		}
		if history[0].Session != "session-1.log" || history[0].TotalCost != 0.1234 {
			t.Errorf("Record mismatch: %+v", history[0])
		}
	})

	// Test 2: Append different session
	t.Run("AppendNew", func(t *testing.T) {
		record := SessionCostRecord{
			Date:      "2026-01-02",
			Session:   "session-2.log",
			Model:     "gemini-test",
			TotalCost: 0.5678,
		}
		m.recordCost(context.Background(), modeDir, "testmode", record)

		history := readHistory()
		if len(history) != 2 {
			t.Fatalf("Expected 2 records, got %d", len(history))
		}
		if history[1].Session != "session-2.log" {
			t.Errorf("Second record mismatch: %+v", history[1])
		}
	})

	// Test 3: Update existing session (idempotency)
	t.Run("UpdateExisting", func(t *testing.T) {
		record := SessionCostRecord{
			Date:      "2026-01-01",
			Session:   "session-1.log",
			Model:     "gemini-test",
			TotalCost: 0.9999, // Updated cost
		}
		m.recordCost(context.Background(), modeDir, "testmode", record)

		history := readHistory()
		if len(history) != 2 {
			t.Fatalf("Expected 2 records, got %d", len(history))
		}
		// Verify session 1 was updated
		found := false
		for _, r := range history {
			if r.Session == "session-1.log" {
				if r.TotalCost != 0.9999 {
					t.Errorf("Cost not updated. Got %f, want 0.9999", r.TotalCost)
				}
				found = true
			}
		}
		if !found {
			t.Error("Session 1 disappeared after update")
		}
	})

	// Test 4: Retention Policy
	t.Run("RetentionPolicy", func(t *testing.T) {
		retentionDir := filepath.Join(tmpDir, "retentionmode")
		os.MkdirAll(retentionDir, 0755)

		// Create config with 1 day retention
		config := map[string]string{"cost_retention_days": "1"}
		configData, _ := json.Marshal(config)
		os.WriteFile(filepath.Join(retentionDir, "config.json"), configData, 0644)

		mRetention := &metricsManager{
			sm:      sm,
			logFile: filepath.Join(retentionDir, "tokens.log"),
			model:   "gemini-test",
			mode:    "retentionmode",
		}

		// Old record (more than 1 day ago)
		oldRecord := SessionCostRecord{
			Date:      "2000-01-01",
			Session:   "old.log",
			Model:     "gemini-test",
			TotalCost: 0.0001,
		}
		// New record
		newRecord := SessionCostRecord{
			Date:      time.Now().Format("2006-01-02"),
			Session:   "new.log",
			Model:     "gemini-test",
			TotalCost: 1.0,
		}

		mRetention.recordCost(context.Background(), retentionDir, "retentionmode", oldRecord)
		mRetention.recordCost(context.Background(), retentionDir, "retentionmode", newRecord)

		history := readHistory()
		// Session 'old.log' should be purged because of the 1-day retention policy
		for _, r := range history {
			if r.Session == "old.log" {
				t.Errorf("Old record should have been purged")
			}
		}
		if len(history) == 0 {
			t.Errorf("Expected at least one record (the new one)")
		}
	})
}

func TestGetCostSummary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metrics_summary_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	modeDir := filepath.Join(tmpDir, "testmode")
	os.MkdirAll(modeDir, 0755)
	logFile := filepath.Join(modeDir, "tokens.log")

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)

	m := &metricsManager{
		sm:      sm,
		logFile: logFile,
		model:   "gemini-test",
		mode:    "testmode",
	}

	ctx := context.Background()

	// Test 1: Empty/No File
	t.Run("NoFile", func(t *testing.T) {
		summary, err := m.getCostSummary(ctx)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !strings.Contains(summary, "No cost history found yet") {
			t.Errorf("Unexpected empty message: %s", summary)
		}
	})

	// Test 2: Generate Summary
	t.Run("GenerateSummary", func(t *testing.T) {
		// Seed data
		records := []SessionCostRecord{
			{Date: "2026-01-01", Session: "s1", TotalCost: 1.0},
			{Date: "2026-01-01", Session: "s2", TotalCost: 2.0}, // Same day
			{Date: "2026-01-02", Session: "s3", TotalCost: 5.0}, // Different day
		}
		data, _ := json.Marshal(records)
		os.WriteFile(filepath.Join(tmpDir, "global_costs.json"), data, 0644)

		summary, err := m.getCostSummary(ctx)
		if err != nil {
			t.Fatalf("getCostSummary failed: %v", err)
		}

		// Verify content
		expectedSubstrings := []string{
			"| 2026-01-02 | $5.0000 |",
			"| 2026-01-01 | $3.0000 |",
			"| **Grand Total** | **$8.0000** |",
		}

		for _, s := range expectedSubstrings {
			if !strings.Contains(summary, s) {
				t.Errorf("Summary missing expected string: %q\nGot:\n%s", s, summary)
			}
		}
	})
}

func TestEstimateCostIntegration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "metrics_estimate_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	modeDir := filepath.Join(tmpDir, "testmode")
	os.MkdirAll(modeDir, 0755)

	// 1. Create a dummy log file
	logContent := `
[10:00:00] H: 1000 M: 500 C: 200 T: 1700 N: 1700(1%) S: 1 Th: 50 [1.00s]
`
	logPath := filepath.Join(modeDir, "test-tokens.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("Failed to write log file: %v", err)
	}

	sm := NewSecurityManager()
	sm.RegisterSafePath(tmpDir)

	m := &metricsManager{
		sm:      sm,
		logFile: logPath,
		model:   "gemini-2.0-flash-001",
		mode:    "testmode",
	}

	// 2. Run EstimateCost (which triggers recordCost)
	summary, err := m.EstimateCost(context.Background(), true, "")
	if err != nil {
		t.Fatalf("EstimateCost failed: %v", err)
	}

	// 3. Verify Output
	if !strings.Contains(summary, "Estimated Cost for Session") {
		t.Errorf("Summary header missing: %s", summary)
	}
	if !strings.Contains(summary, "Total") {
		t.Errorf("Total missing: %s", summary)
	}

	// 4. Verify Side Effect (File Creation)
	historyPath := filepath.Join(tmpDir, "global_costs.json")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		t.Error("global_costs.json was not created")
	}

	// 5. Verify Content of Ledger
	data, _ := os.ReadFile(historyPath)
	var history []SessionCostRecord
	json.Unmarshal(data, &history)
	if len(history) != 1 {
		t.Errorf("Expected 1 history record, got %d", len(history))
	} else {
		if history[0].Session != "test-tokens.log" {
			t.Errorf("Session name mismatch: %s", history[0].Session)
		}
		if history[0].TotalCost <= 0 {
			t.Errorf("Total cost should be > 0, got %f", history[0].TotalCost)
		}
	}
}
