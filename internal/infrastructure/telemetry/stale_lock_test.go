// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestRecordCost_BreaksStaleLock(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "record_cost_stale_lock_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

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
		sm: security.NewSecurityManager(nil),
	}

	record := sessionCostRecord{
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
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "recover_ledger_model_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

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

	sm := security.NewSecurityManager(nil)
	m := &metricsManager{
		sm:    sm,
		model: "default-model", // This is the current session model
	}

	// We need a pricing override for "gpt-4-special" to test recalculation
	m.pricingOverrides = map[string]domain_pricing.ModelPricing{
		"gpt-4-special": {
			Hit:  10.0,
			Miss: 100.0,
			Comp: 200.0,
		},
	}
	m.ledger = newLedgerStore(sm, m.model, m.pricingOverrides)

	m.ledger.recoverLedger(context.Background(), globalDir)

	// Wait for background recovery (though here it might be sync if it's small)
	// Actually recoverLedger is sync when called directly like this, but wait, it uses a sync.Map to prevent double recovery.

	historyPath := filepath.Join(globalDir, "global_costs.json")
	content, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}

	var history []sessionCostRecord
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
