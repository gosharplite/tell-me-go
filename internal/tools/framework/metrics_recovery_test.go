// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		sm:    sm,
		model: "test-model",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	m.recoverLedger(ctx, tempDir)
	
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
		mode: "test-mode",
	}

	outputDir := filepath.Join(tempDir, "test-mode")
	_ = os.MkdirAll(outputDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	record := SessionCostRecord{
		Date: "2023-10-27",
		Session: "test-session",
		TotalCost: 1.0,
	}

	m.recordCost(ctx, outputDir, "test-mode", record)

	historyPath := filepath.Join(tempDir, "global_costs.json")
	if _, err := os.Stat(historyPath); err == nil {
		t.Errorf("global_costs.json should not have been created because context was cancelled")
	}
}
