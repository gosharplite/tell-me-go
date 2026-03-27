// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitLogger(t *testing.T) {
	closer, err := InitLogger()
	if err != nil {
		t.Fatalf("InitLogger() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("failed to close logger: %v", err)
		}
	})

	expectedPath := filepath.Join(os.TempDir(), "tell-me-go-tui.log")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected log file at %s, but it was not found", expectedPath)
	}
}
