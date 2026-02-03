// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestRecoverLedger_ContextCancellation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "metrics_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create some dummy log files to make the walk take some time
	for i := 0; i < 10; i++ {
		path := filepath.Join(tempDir, "subdir", "session_tokens.log")
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		_ = os.WriteFile(path, []byte("{}"), 0644)
	}

	sm := security.NewSecurityManager(strings.NewReader(""))
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:     sm,
		model:  "test-model",
		ledger: NewLedgerStore(sm, "test-model", nil),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	m.ledger.RecoverLedger(ctx, tempDir)

	// The ledger should NOT have been created if it was cancelled immediately
	historyPath := filepath.Join(tempDir, "global_costs.json")
	if _, err := os.Stat(historyPath); err == nil {
		t.Errorf("global_costs.json should not have been created because context was cancelled")
	}
}

func TestRecordCost_UsesContext(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "metrics_test_record")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	sm := security.NewSecurityManager(strings.NewReader(""))
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:    sm,
		model: "test-model",
		mode:  "test-mode",
	}

	outputDir := filepath.Join(tempDir, "test-mode")
	_ = os.MkdirAll(outputDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	record := SessionCostRecord{
		Date:      "2023-10-27",
		Session:   "test-session",
		TotalCost: 1.0,
	}

	m.recordCost(ctx, outputDir, "test-mode", record)

	historyPath := filepath.Join(tempDir, "global_costs.json")
	if _, err := os.Stat(historyPath); err == nil {
		t.Errorf("global_costs.json should not have been created because context was cancelled")
	}
}

func TestRecordCost_RecoveryContinuesOnContextCancel(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "metrics_test_recovery_bg")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create a log file so recovery has something to do
	logPath := filepath.Join(tempDir, "test-mode", "session_tokens.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)
	_ = os.WriteFile(logPath, []byte("{}"), 0644)

	sm := security.NewSecurityManager(strings.NewReader(""))
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:     sm,
		model:  "test-model",
		mode:   "test-mode",
		ledger: NewLedgerStore(sm, "test-model", nil),
	}

	outputDir := filepath.Join(tempDir, "test-mode")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	record := SessionCostRecord{
		Date:      "2023-10-27",
		Session:   "test-session",
		TotalCost: 1.0,
	}

	// This should trigger recovery in background.
	// Now it should SUCCEED because it uses a decoupled background context.
	m.recordCost(ctx, outputDir, "test-mode", record)

	// Since it's in background, we might need to wait a bit
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Poll for file existence
	success := false
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(historyPath); err == nil {
			success = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !success {
		t.Errorf("global_costs.json should have been created by background recovery despite cancelled parent context")
	}
}
