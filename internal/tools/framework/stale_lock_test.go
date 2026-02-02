// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestBreakStaleLock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "stale_lock_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	lockPath := filepath.Join(tmpDir, "test.lock")

	// 1. Create a fresh lock
	err = os.WriteFile(lockPath, []byte("lock"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 2. breakStaleLock should NOT remove it if it's new
	breakStaleLock(lockPath)
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("breakStaleLock removed a fresh lock")
	}

	// 3. Make it old
	oldTime := time.Now().Add(-10 * time.Minute)
	err = os.Chtimes(lockPath, oldTime, oldTime)
	if err != nil {
		t.Fatal(err)
	}

	// 4. breakStaleLock should now remove it
	breakStaleLock(lockPath)
	if _, err := os.Stat(lockPath); err == nil {
		t.Error("breakStaleLock failed to remove a stale lock")
	}
}

func TestRecordCost_BreaksStaleLock(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "record_cost_stale_lock_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	outputDir := filepath.Join(tmpDir, "coder")
	_ = os.MkdirAll(outputDir, 0755)

	globalDir := tmpDir
	historyPath := filepath.Join(globalDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// Create a stale lock
	_ = os.WriteFile(lockPath, []byte("stale"), 0644)
	oldTime := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(lockPath, oldTime, oldTime)

	m := &metricsManager{
		sm: security.NewSecurityManager(os.Stdin),
	}

	record := SessionCostRecord{
		Date:      "2026-02-02",
		Session:   "test-session",
		Model:     "test-model",
		TotalCost: 1.23,
	}

	// recordCost should succeed because it breaks the stale lock
	m.recordCost(context.Background(), outputDir, "coder", record)

	// Verify the lock is gone (removed after success)
	if _, err := os.Stat(lockPath); err == nil {
		t.Error("Lock file still exists after recordCost")
	}

	// Verify history was written
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		t.Error("history file was not written")
	}
}

func TestRecoverLedger_DetectedModel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "recover_ledger_model_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	globalDir := tmpDir
	sessionDir := filepath.Join(globalDir, "coder", "20260202_120000")
	_ = os.MkdirAll(sessionDir, 0755)

	logPath := filepath.Join(sessionDir, "tokens.log")

	// Create a log with a specific model in JSON
	metrics := llm.Metrics{
		Model:          "gpt-4-special",
		CachedTokens:   100,
		PromptTokens:   200,
		ResponseTokens: 50,
	}
	data, _ := json.Marshal(metrics)
	_ = os.WriteFile(logPath, append(data, '\n'), 0644)

	m := &metricsManager{
		sm:    security.NewSecurityManager(os.Stdin),
		model: "default-model", // This is the current session model
	}

	// We need a pricing override for "gpt-4-special" to test recalculation
	m.pricingOverrides = map[string]llm.ModelPricing{
		"gpt-4-special": {
			Hit:  10.0,
			Miss: 100.0,
			Comp: 200.0,
		},
	}

	m.recoverLedger(context.Background(), globalDir)

	// Wait for background recovery (though here it might be sync if it's small)
	// Actually recoverLedger is sync when called directly like this, but wait, it uses a sync.Map to prevent double recovery.

	historyPath := filepath.Join(globalDir, "global_costs.json")
	content, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}

	var history []SessionCostRecord
	_ = json.Unmarshal(content, &history)

	if len(history) == 0 {
		t.Fatal("History not recovered")
	}

	found := false
	for _, r := range history {
		if r.Model == "gpt-4-special" {
			found = true
			// Cost for gpt-4-special:
			// Hit: 100 * 10 / 1e6 = 0.001
			// Miss: (200-100) * 100 / 1e6 = 0.01
			// Comp: 50 * 200 / 1e6 = 0.01
			// Total: 0.021
			if r.TotalCost == 0 {
				t.Errorf("Recovered cost is 0")
			}
			// Let's check exact cost if possible
			expected := (100.0*10.0 + 100.0*100.0 + 50.0*200.0) / 1e6
			if math.Abs(r.TotalCost-expected) > 1e-9 {
				t.Errorf("Expected cost %f, got %f", expected, r.TotalCost)
			}
		}
	}

	if !found {
		t.Error("Recovered record has wrong model or was not found")
	}
}
