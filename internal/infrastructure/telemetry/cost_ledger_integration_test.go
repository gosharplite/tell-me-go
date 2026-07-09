// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCostLedger_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")

	original := []sessionCostRecord{
		{Date: "2026-01-27", Session: "test-session", Model: "gpt-4", TotalCost: 0.50},
		{Date: "2026-01-27", Session: "another-session", Model: "gemini-pro", TotalCost: 0.0},
	}
	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(historyPath, data, 0644))

	records, fileExisted, err := loadHistoryFromDisk(historyPath)
	require.NoError(t, err)
	assert.True(t, fileExisted)
	assert.Len(t, records, 2)
	assert.Equal(t, "test-session", records[0].Session)
	assert.Equal(t, 0.50, records[0].TotalCost)
}

func TestCostLedger_MissingFileIsGraceful(t *testing.T) {
	t.Parallel()
	nonexistentPath := filepath.Join(t.TempDir(), "nonexistent", "global_costs.json")
	records, fileExisted, err := loadHistoryFromDisk(nonexistentPath)
	assert.NoError(t, err)
	assert.False(t, fileExisted)
	assert.Empty(t, records)
}

func TestCostLedger_CorruptedFileIsRecovered(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.NoError(t, os.WriteFile(historyPath, []byte("{this is not valid json"), 0644))
	records, fileExisted, err := loadHistoryFromDisk(historyPath)
	assert.NoError(t, err)
	assert.True(t, fileExisted)
	assert.Empty(t, records)
	_, err = os.Stat(historyPath + ".bak")
	assert.NoError(t, err, "corrupted file should be backed up as .bak")
}
