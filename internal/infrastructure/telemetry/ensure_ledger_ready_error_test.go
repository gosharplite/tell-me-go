// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureLedgerReady(t *testing.T) {
	t.Run("unmarshal_error", func(t *testing.T) {
		tempDir := t.TempDir()

		outputDir := filepath.Join(tempDir, "mode")
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatal(err)
		}

		historyPath := filepath.Join(tempDir, "global_costs.json")
		// Valid JSON but wrong shape: an object instead of an array.
		if err := os.WriteFile(historyPath, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		m := &metricsManager{
			logFile: filepath.Join(outputDir, "tokens.log"),
		}

		history, status, err := m.ensureLedgerReady(context.Background(), historyPath, tempDir)

		if history != nil {
			t.Errorf("expected nil history, got %v", history)
		}

		wantStatus := "Error parsing cost history. The file may be corrupted."
		if status != wantStatus {
			t.Errorf("unexpected status: got %q, want %q", status, wantStatus)
		}

		if err == nil {
			t.Fatal("expected non-nil error for JSON unmarshal failure")
		}
	})
}
