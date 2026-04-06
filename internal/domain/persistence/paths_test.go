// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolvePaths(t *testing.T) {
	homeDir := "/home/user"

	tests := []struct {
		name     string
		mode     string
		wantMode string
	}{
		{"Normal mode", "assistant", "assistant"},
		{"Path traversal - dots", "..", "default"},
		{"Path traversal - slash", "/", "default"},
		{"Path traversal - backslash", "\\", "default"},
		{"Empty mode", "", "default"},
		{"Subdirectory traversal", "foo/bar", "bar"},
		{"Dotted mode", ".", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := ResolvePaths(homeDir, tt.mode)
			expectedDir := filepath.Join(homeDir, "output", tt.wantMode)
			assert.Equal(t, expectedDir, paths.ModeDir, "ModeDir mismatch for mode %q", tt.mode)
			assert.Contains(t, paths.HistoryPath, expectedDir)
			assert.Contains(t, paths.HistoryArchivePath, expectedDir)
			assert.Contains(t, paths.LogPath, expectedDir)
			assert.Contains(t, paths.TracePath, expectedDir)
			assert.Contains(t, paths.CommandsLogPath, expectedDir)
			assert.Contains(t, paths.TurnsLogPath, expectedDir)
		})
	}
}
