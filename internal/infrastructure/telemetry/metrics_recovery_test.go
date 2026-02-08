// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRecoverLedger_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setup          func(t *testing.T, baseDir string)
		expectedCount  int
		expectedCost   float64
		validateResult func(t *testing.T, results []SessionCostRecord)
	}{
		{
			name: "Corrupted Log",
			setup: func(t *testing.T, baseDir string) {
				logDir := filepath.Join(baseDir, "mode1")
				require.NoError(t, os.MkdirAll(logDir, 0755))
				content := `{"prompt_tokens": 100, "response_tokens": 50}
invalid json line
{"prompt_tokens": 200, "response_tokens": 100}`
				require.NoError(t, os.WriteFile(filepath.Join(logDir, "tokens.log"), []byte(content), 0644))
			},
			expectedCount: 1,
			validateResult: func(t *testing.T, results []SessionCostRecord) {
				assert.Equal(t, "mode1/tokens.log", results[0].Session)
				// 100+200 prompt, 50+100 response.
				// Pricing for default model is needed to check exact cost.
			},
		},
		{
			name: "Duplicate Sessions",
			setup: func(t *testing.T, baseDir string) {
				// Same session in backups and main mode dir
				backupDir := filepath.Join(baseDir, "backups/2023/10/27/mode1")
				mainDir := filepath.Join(baseDir, "mode1")
				require.NoError(t, os.MkdirAll(backupDir, 0755))
				require.NoError(t, os.MkdirAll(mainDir, 0755))

				logContent := `{"prompt_tokens": 100, "response_tokens": 50}`
				// Both have same relative path inside their respective roots?
				// Actually getSessionID logic:
				// if starts with backups/ -> backup/ + rel
				// else -> rel
				// So they might NOT be considered duplicates if one is in backups/ and other is not,
				// UNLESS they have the same session ID.
				// getSessionID(path, globalDir)

				require.NoError(t, os.WriteFile(filepath.Join(backupDir, "tokens.log"), []byte(logContent), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(mainDir, "tokens.log"), []byte(logContent), 0644))
			},
			expectedCount: 2, // Currently they are distinct: "backup/2023/10/27/mode1/tokens.log" vs "mode1/tokens.log"
		},
		{
			name: "Missing Models",
			setup: func(t *testing.T, baseDir string) {
				logDir := filepath.Join(baseDir, "mode2")
				require.NoError(t, os.MkdirAll(logDir, 0755))
				content := `{"prompt_tokens": 100, "response_tokens": 50}` // No model field
				require.NoError(t, os.WriteFile(filepath.Join(logDir, "tokens.log"), []byte(content), 0644))
			},
			expectedCount: 1,
			validateResult: func(t *testing.T, results []SessionCostRecord) {
				assert.Equal(t, "test-model", results[0].Model)
			},
		},
		{
			name: "Deep Hierarchy",
			setup: func(t *testing.T, baseDir string) {
				deepDir := filepath.Join(baseDir, "backups/2023/11/01/deep/path/mode3")
				require.NoError(t, os.MkdirAll(deepDir, 0755))
				content := `{"prompt_tokens": 10, "response_tokens": 5}`
				require.NoError(t, os.WriteFile(filepath.Join(deepDir, "tokens.log"), []byte(content), 0644))
			},
			expectedCount: 1,
			validateResult: func(t *testing.T, results []SessionCostRecord) {
				assert.Contains(t, results[0].Session, "backup/2023/11/01/deep/path/mode3/tokens.log")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			tt.setup(t, tempDir)

			sm := security.NewSecurityManager(strings.NewReader(""))
			sm.RegisterSafePath(tempDir)
			ls := NewLedgerStore(sm, "test-model", nil)

			ls.RecoverLedger(context.Background(), tempDir)

			historyPath := filepath.Join(tempDir, "global_costs.json")
			content, err := os.ReadFile(historyPath)
			require.NoError(t, err)

			var results []SessionCostRecord
			require.NoError(t, json.Unmarshal(content, &results))

			assert.Len(t, results, tt.expectedCount)
			if tt.validateResult != nil {
				tt.validateResult(t, results)
			}
		})
	}
}
