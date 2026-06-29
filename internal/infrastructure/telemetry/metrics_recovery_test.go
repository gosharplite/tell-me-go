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
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "metrics_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create some dummy log files to make the walk take some time
	for i := 0; i < 10; i++ {
		path := filepath.Join(tempDir, "subdir", "session_tokens.log")
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		_ = os.WriteFile(path, []byte("{}"), 0644)
	}

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:     sm,
		model:  "test-model",
		ledger: newLedgerStore(sm, "test-model", nil),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	m.ledger.recoverLedger(ctx, tempDir)

	// The ledger should NOT have been created if it was cancelled immediately
	historyPath := filepath.Join(tempDir, "global_costs.json")
	if _, err := os.Stat(historyPath); err == nil {
		t.Errorf("global_costs.json should not have been created because context was cancelled")
	}
}

func TestRecordCost_UsesContext(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "metrics_test_record")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := security.NewSecurityManager(nil)
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

	record := sessionCostRecord{
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
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "metrics_test_recovery_bg")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create a log file so recovery has something to do
	logPath := filepath.Join(tempDir, "test-mode", "session_tokens.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)
	_ = os.WriteFile(logPath, []byte("{}"), 0644)

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)
	m := &metricsManager{
		sm:     sm,
		model:  "test-model",
		mode:   "test-mode",
		ledger: newLedgerStore(sm, "test-model", nil),
	}

	outputDir := filepath.Join(tempDir, "test-mode")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	record := sessionCostRecord{
		Date:      "2023-10-27",
		Session:   "test-session",
		TotalCost: 1.0,
	}

	// This should trigger recovery in background.
	// Now it should SUCCEED because it uses a decoupled background context.
	m.recordCost(ctx, outputDir, "test-mode", record)

	// Since it's in background, we might need to wait a bit
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Wait for background recovery to create the global_costs.json file
	require.Eventually(t, func() bool {
		_, err := os.Stat(historyPath)
		return err == nil
	}, 5*time.Second, 50*time.Millisecond, "global_costs.json should have been created by background recovery")
}

func TestRecoverLedger_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setup          func(t *testing.T, baseDir string)
		expectedCount  int
		expectedCost   float64
		validateResult func(t *testing.T, results []sessionCostRecord)
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
			validateResult: func(t *testing.T, results []sessionCostRecord) {
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
			validateResult: func(t *testing.T, results []sessionCostRecord) {
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
			validateResult: func(t *testing.T, results []sessionCostRecord) {
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

			sm := security.NewSecurityManager(nil)
			sm.RegisterSafePath(tempDir)
			ls := newLedgerStore(sm, "test-model", nil)

			ls.recoverLedger(context.Background(), tempDir)

			historyPath := filepath.Join(tempDir, "global_costs.json")
			content, err := os.ReadFile(historyPath)
			require.NoError(t, err)

			var results []sessionCostRecord
			require.NoError(t, json.Unmarshal(content, &results))

			assert.Len(t, results, tt.expectedCount)
			if tt.validateResult != nil {
				tt.validateResult(t, results)
			}
		})
	}
}

// TestRegisterMetrics_UnmarshalArgsErrors exercises the UnmarshalArgs error
// paths inside the RegisterMetrics closures at metrics.go:65-67 (estimate_cost)
// and metrics.go:104-106 (get_cost_summary). These paths are reachable via:
//   - Non-JSON-serializable values (e.g., chan) → json.Marshal fails
//   - Type-mismatched JSON values (e.g., string where bool is expected) → json.Unmarshal fails
//
// The handlers wrap these errors with "invalid arguments: %w" and return them.
func TestRegisterMetrics_UnmarshalArgsErrors(t *testing.T) {
	// NOT parallel: subtests share a single mockRegistry with registered handlers.
	sm := &mockSM{}

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")
	traceFile := filepath.Join(outputDir, "test.trace.jsonl")

	reg := &mockRegistry{}
	if err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil, nil); err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}

	tests := []struct {
		name        string
		toolName    string
		args        map[string]interface{}
		wantErrText string
	}{
		{
			name:        "estimate_cost with non-serializable args",
			toolName:    "estimate_cost",
			args:        map[string]interface{}{"bad": make(chan int)},
			wantErrText: "invalid arguments",
		},
		{
			name:        "get_cost_summary with string billing",
			toolName:    "get_cost_summary",
			args:        map[string]interface{}{"billing": "not-a-bool"},
			wantErrText: "invalid arguments",
		},
		{
			name:        "get_cost_summary with numeric billing",
			toolName:    "get_cost_summary",
			args:        map[string]interface{}{"billing": 123},
			wantErrText: "invalid arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := reg.handlers[tt.toolName]
			if handler == nil {
				t.Fatalf("%s handler not registered", tt.toolName)
			}

			_, err := handler(context.Background(), tt.args, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("error should contain %q, got: %v", tt.wantErrText, err)
			}
		})
	}
}
