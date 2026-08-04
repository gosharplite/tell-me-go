// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// generateSessionID with backup paths (direct unit test)
// ---------------------------------------------------------------------------

func TestGenerateSessionID_BackupPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     string
		logFile  string
		expected string
	}{
		{
			name:     "standard path (no backups)",
			mode:     "interactive",
			logFile:  "/data/interactive/2023/10/27/tokens.log",
			expected: "interactive/tokens.log",
		},
		{
			name:     "backup path with backups/ prefix",
			mode:     "mode",
			logFile:  "/tmp/mode/backups/2023/mode/session_tokens.log",
			expected: "mode/session_tokens.log",
		},
		{
			name:     "deeply nested backup path",
			mode:     "automated",
			logFile:  "/var/data/automated/backups/2024/01/15/automated/turn_tokens.log",
			expected: "automated/turn_tokens.log",
		},
		{
			name:     "backup path without mode directory prefix",
			mode:     "mode",
			logFile:  "/tmp/backups/2023/other/session_tokens.log",
			expected: "mode/session_tokens.log",
		},
		{
			name:     "empty mode",
			mode:     "",
			logFile:  "/tmp/backups/2023/empty/tokens.log",
			expected: "tokens.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := generateSessionID(tt.mode, tt.logFile)
			assert.Equal(t, tt.expected, got)
		})
	}
}
