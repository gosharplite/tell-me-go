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
)

// TestArchiveCostPreservation verifies that starting a new session (-new)
// preserves the cost of the archived session in the ledger under a unique ID.
func TestArchiveCostPreservation(t *testing.T) {
	// 1. Build the binary
	binaryPath := filepath.Join(os.TempDir(), "tell-me-go-e2e")
	cmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/tell-me-go/")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer os.Remove(binaryPath)

	// 2. Setup Test Environment
	tmpHome, err := os.MkdirTemp("", "archive_cost_home")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	if err := os.Setenv("TELL_ME_HOME", tmpHome); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TELL_ME_HOME")

	outputDir := filepath.Join(tmpHome, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a dummy config
	configDir := filepath.Join(tmpHome, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "test.yaml")
	configContent := `
MODE: "test"
AIMODEL: "gemini-test-flash"
AIURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1/publishers/google/models"
MAX_HISTORY_TURNS: 10
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Seed the ledger with a "previous" session cost
	ledgerPath := filepath.Join(outputDir, "global_costs.json")
	initialLedger := []map[string]interface{}{
		{
			"date":       "2026-01-28",
			"session":    "tokens.log",
			"model":      "gemini-test-flash",
			"total_cost": 1.2345,
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
	logFile := filepath.Join(modeDir, "tokens.log")
	logContent := "[10:00:00] H: 100 M: 100 C: 100 T: 300 N: 300(1%) S: 0 Th: 0 [1.00s]\n"
	if err := os.WriteFile(logFile, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 4. Run the tool with -new flag to trigger archiving
	if err := os.Setenv("TELL_ME_MOCK_ANSWER", "Acknowledged."); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")
	if err := os.Setenv("TELL_ME_MOCK_URL", "http://localhost:9999"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TELL_ME_MOCK_URL")

	cmd = exec.Command(binaryPath, "-c", configPath, "-new", "New session start")
	_ = cmd.Run() // Expected to fail after archiving due to mock server not existing, but we only care about the archiving logic here.

	// 5. Verify the ledger (global_costs.json)
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("Failed to read ledger: %v", err)
	}

	type SessionCostRecord struct {
		Session   string  `json:"session"`
		TotalCost float64 `json:"total_cost"`
	}
	var history []SessionCostRecord
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatalf("Failed to parse ledger: %v", err)
	}

	// We expect:
	// 1. The original entry (test_tokens.log) with cost 1.2345
	// 2. A NEW backup entry (backup/.../test_tokens.log) with recalculated cost from log file

	hasBackup := false
	originalPreserved := false

	for _, r := range history {
		if r.Session == "tokens.log" && r.TotalCost == 1.2345 {
			originalPreserved = true
		}
		if strings.HasPrefix(r.Session, "backup/") && filepath.Base(r.Session) == "tokens.log" {
			hasBackup = true
		}
	}

	if !hasBackup {
		t.Errorf("Ledger missing archived session backup. History: %+v", history)
	}
	if !originalPreserved {
		t.Errorf("The original session entry was overwritten or deleted. History: %+v", history)
	}

	t.Logf("Ledger verification successful: %+v", history)
}
