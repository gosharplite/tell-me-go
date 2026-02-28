// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveCostPreservation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	binaryPath, tmpHome, configPath := setupTestEnvironment(t)

	outputDir := filepath.Join(tmpHome, "output")
	ledgerPath := filepath.Join(outputDir, "global_costs.json")

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
		t.Run(tc.name, func(t *testing.T) {
			// Clear output directory for a fresh subtest state
			os.RemoveAll(outputDir)
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				t.Fatal(err)
			}

			// 1. Seed the ledger with a "previous" session cost
			seedPreviousSession(t, outputDir, "tokens.log", tc.initialCost)

			// 2. Run the tool with -new flag to trigger archiving
			runAppWithNewSession(t, binaryPath, configPath)

			// 3. Verify the ledger (global_costs.json)
			verifyCostLedger(t, ledgerPath, "tokens.log", tc.initialCost)
		})
	}
}

func setupTestEnvironment(t *testing.T) (binaryPath, tmpHome, configPath string) {
	t.Helper()

	// Build the binary
	binaryPath = filepath.Join(os.TempDir(), "tell-me-go-e2e")
	cmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/tell-me-go/")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	t.Cleanup(func() { os.Remove(binaryPath) })

	// Setup Test Home
	var err error
	tmpHome, err = os.MkdirTemp("", "archive_cost_home")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpHome) })

	if err := os.Setenv("TELL_ME_HOME", tmpHome); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("TELL_ME_HOME") })

	// Create directories
	outputDir := filepath.Join(tmpHome, "output")
	configDir := filepath.Join(tmpHome, "configs")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a dummy config
	configPath = filepath.Join(configDir, "test.yaml")
	configContent := `
MODE: "test"
AIMODEL: "gemini-test-flash"
AIURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/publishers/google/models"
MAX_HISTORY_TURNS: 10
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	return binaryPath, tmpHome, configPath
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
	ledgerBytes, err := json.MarshalIndent(initialLedger, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, ledgerBytes, 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate a previous session log file
	modeDir := filepath.Join(outputDir, "test")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(modeDir, sessionName)
	logContent := "[10:00:00] H: 100 M: 100 C: 100 T: 300 N: 300(1%) S: 0 Th: 0 [1.00s]\n"
	if err := os.WriteFile(logFile, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}
}

func runAppWithNewSession(t *testing.T, binaryPath, configPath string) {
	t.Helper()

	if err := os.Setenv("TELL_ME_MOCK_ANSWER", "Acknowledged."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("TELL_ME_MOCK_ANSWER") })
	if err := os.Setenv("TELL_ME_MOCK_URL", "http://localhost:9999"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("TELL_ME_MOCK_URL") })

	cmd := exec.Command(binaryPath, "-c", configPath, "-new", "New session start")
	// Expected to fail after archiving due to mock server not existing, but we only care about the archiving logic here.
	_ = cmd.Run()
}

type sessionCostRecord struct {
	Session   string  `json:"session"`
	TotalCost float64 `json:"total_cost"`
}

func verifyCostLedger(t *testing.T, ledgerPath string, originalSession string, originalCost float64) {
	t.Helper()

	history := loadLedger(t, ledgerPath)

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

func loadLedger(t *testing.T, path string) []sessionCostRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read ledger: %v", err)
	}
	var history []sessionCostRecord
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatalf("Failed to parse ledger: %v", err)
	}
	return history
}

func isBackupOf(sessionPath, originalName string) bool {
	return strings.HasPrefix(sessionPath, "backup/") && filepath.Base(sessionPath) == originalName
}
