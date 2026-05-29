// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadHistory_RenameBackupFailure covers the os.Rename error path inside
// loadHistory when the directory is read-only. The existing
// TestMetricsManager_LoadHistory_Corrupted covers the success-path rename;
// this test targets the inner failure branch (metrics_cost.go:55-56).
func TestLoadHistory_RenameBackupFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent file renames on Windows")
	}

	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")

	// 1. Create a corrupted JSON file.
	corruptedContent := `"{{{broken"`
	require.NoError(t, os.WriteFile(historyPath, []byte(corruptedContent), 0644))

	// 2. Make the directory read-only so os.Rename fails (needs write on dir).
	require.NoError(t, os.Chmod(tempDir, 0555))
	t.Cleanup(func() { _ = os.Chmod(tempDir, 0755) })

	// 3. Call loadHistory — must not panic.
	m := &metricsManager{}
	result := m.loadHistory(context.Background(), historyPath, tempDir)

	// 4. Returns an empty slice (not nil).
	assert.Equal(t, 0, len(result), "expected empty slice, got len=%d", len(result))

	// 5. The .bak file should NOT exist (rename failed).
	_, err := os.Stat(historyPath + ".bak")
	assert.True(t, os.IsNotExist(err), "expected .bak file to not exist, but it does")

	// 6. The original corrupted file should still exist with original content.
	data, err := os.ReadFile(historyPath)
	require.NoError(t, err, "original file should still be readable")
	assert.Equal(t, corruptedContent, string(data), "original file content should be unchanged")
}
