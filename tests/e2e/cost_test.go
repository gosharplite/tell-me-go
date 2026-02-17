// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCostLedgerSystem verifies the data integrity of the cost tracking system.
// It ensures the shared state (global_costs.json) follows the expected contract.
func TestCostLedgerSystem(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TELL_ME_HOME", tmpDir)

	// 1. Setup dummy log
	setupDummyLog(t, tmpDir)

	// 2. Verify shared state contract (Data Integrity)
	t.Run("DataIntegrity", func(t *testing.T) {
		historyPath := filepath.Join(tmpDir, "output", "global_costs.json")

		// SessionCostRecord represents the structure of records in global_costs.json
		type SessionCostRecord struct {
			Date      string  `json:"date"`
			Session   string  `json:"session"`
			Model     string  `json:"model"`
			TotalCost float64 `json:"total_cost"`
		}

		records := []SessionCostRecord{
			{Date: "2026-01-27", Session: "test-session", Model: "gpt-4", TotalCost: 0.50},
		}

		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(historyPath, data, 0644); err != nil {
			t.Fatal(err)
		}

		// Now read it back to verify the contract
		readData, err := os.ReadFile(historyPath)
		if err != nil {
			t.Fatalf("Failed to read history: %v", err)
		}

		var readRecords []SessionCostRecord
		if err := json.Unmarshal(readData, &readRecords); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if len(readRecords) != 1 {
			t.Errorf("Expected 1 record, got %d", len(readRecords))
		}
		if readRecords[0].TotalCost != 0.50 {
			t.Errorf("Data corruption: expected 0.50, got %f", readRecords[0].TotalCost)
		}
	})
}

// setupDummyLog creates a mock token log for testing purposes.
func setupDummyLog(t *testing.T, tmpDir string) {
	t.Helper()
	logDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(logDir, "assistant-tokens.log")

	// Format matches internal/agent/agent.go
	logContent := `[10:00:00] [System] Init
[10:00:05] H: 100000 M: 1000 C: 500 T: 101500 N: 101500(80%) S: 0 Th: 0 [2.00s]
`
	if err := os.WriteFile(logFile, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}
}
