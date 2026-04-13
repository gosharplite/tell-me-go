// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveCostPreservation(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	tests := []struct {
		name        string
		initialCost float64
	}{
		{
			name:        "Standard cost preservation",
			initialCost: 1.2345,
		},
		{
			name:        "Zero cost preservation",
			initialCost: 0.0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// 1. Setup Hermetic Environment
			tmpHome := t.TempDir()
			outputDir := filepath.Join(tmpHome, "output")
			configDir := filepath.Join(tmpHome, "configs")
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(configDir, 0755); err != nil {
				t.Fatal(err)
			}

			// Create a dummy config
			configPath := filepath.Join(configDir, "test.yaml")
			configContent := "MODE: \"test\""
			if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
				t.Fatal(err)
			}

			ledgerPath := filepath.Join(outputDir, "global_costs.json")

			// 2. Seed the ledger with a "previous" session cost
			seedPreviousSession(t, outputDir, "tokens.log", tc.initialCost)

			// 3. Run the tool with -new flag to trigger archiving
			env := []string{
				"TELL_ME_HOME=" + tmpHome,
				"TELL_ME_MOCK_ANSWER=Acknowledged.",
				"TELL_ME_MOCK_URL=http://localhost:9999", // Fail gracefully
			}
			_, _, _ = runCommandWithEnv(env, "", "-c", configPath, "--new", "New session start")

			// 4. Verify the ledger (global_costs.json)
			verifyCostLedger(t, ledgerPath, "tokens.log", tc.initialCost)
		})
	}
}

func seedPreviousSession(t *testing.T, outputDir, sessionName string, cost float64) {
	t.Helper()

	ledgerPath := filepath.Join(outputDir, "global_costs.json")
	initialLedger := []map[string]interface{}{
		{
			"date":       time.Now().Format("2006-01-02"),
			"session":    sessionName,
			"model":      "gemini-test-flash",
			"total_cost": cost,
		},
	}
	ledgerBytes, _ := json.MarshalIndent(initialLedger, "", "  ")
	_ = os.WriteFile(ledgerPath, ledgerBytes, 0644)

	// Simulate a previous session log file
	modeDir := filepath.Join(outputDir, "test")
	_ = os.MkdirAll(modeDir, 0755)
	logFile := filepath.Join(modeDir, sessionName)
	logContent := "[10:00:00] H: 100 M: 100 C: 100 T: 300 N: 300(1%) S: 0 Th: 0 [1.00s]\n"
	_ = os.WriteFile(logFile, []byte(logContent), 0644)
}

type sessionCostRecord struct {
	Session   string  `json:"session"`
	TotalCost float64 `json:"total_cost"`
}

func verifyCostLedger(t *testing.T, ledgerPath string, originalSession string, originalCost float64) {
	t.Helper()

	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("Failed to read ledger: %v", err)
	}
	var history []sessionCostRecord
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatalf("Failed to parse ledger: %v", err)
	}

	var hasBackup, originalPreserved bool
	for _, r := range history {
		if r.Session == originalSession && r.TotalCost == originalCost {
			originalPreserved = true
		}
		if isBackupOf(r.Session, originalSession) {
			hasBackup = true
		}
	}

	if !hasBackup {
		t.Errorf("Ledger missing archived session backup. History: %+v", history)
	}
	if !originalPreserved {
		t.Errorf("The original session entry was overwritten or deleted. History: %+v", history)
	}
}

func isBackupOf(sessionPath, originalName string) bool {
	return strings.HasPrefix(sessionPath, "backup/") && filepath.Base(sessionPath) == originalName
}
