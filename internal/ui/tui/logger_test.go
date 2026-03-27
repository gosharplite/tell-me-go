// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestInitLogger(t *testing.T) {
	// Capture original state
	originalOut := log.Writer()
	originalFlags := log.Flags()

	closer, err := InitLogger()
	if err != nil {
		t.Fatalf("InitLogger() failed: %v", err)
	}

	// Verify that the global logger has been changed
	if log.Writer() == originalOut {
		t.Error("InitLogger() did not change the global logger output")
	}

	if err := closer.Close(); err != nil {
		t.Errorf("failed to close logger: %v", err)
	}

	// Verify that the global logger has been restored
	if log.Writer() != originalOut {
		t.Error("Close() did not restore the global logger output")
	}
	if log.Flags() != originalFlags {
		t.Error("Close() did not restore the global logger flags")
	}

	expectedPath := filepath.Join(os.TempDir(), "tell-me-go-tui.log")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected log file at %s, but it was not found", expectedPath)
	}
}
