// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

// TestInitLogger_ErrorPath verifies InitLogger returns an error when the
// log file cannot be created (e.g., when TMPDIR points to a file, not a directory).
func TestInitLogger_ErrorPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("setting TMPDIR to a file path does not produce expected error on Windows")
	}

	// Create a regular file and set it as TMPDIR. Since it's a file
	// and not a directory, tea.LogToFile will fail trying to create
	// a log file inside it.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(tmpFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmpFile)

	closer, err := InitLogger()
	if err == nil {
		if closer != nil {
			_ = closer.Close()
		}
		t.Fatal("expected error when TMPDIR is a file, got nil")
	}
	if closer != nil {
		t.Error("expected nil closer on error, got non-nil")
	}
	// Verify the returned type matches io.Closer interface expectation
	var _ io.Closer = nil // compile-time check that nil satisfies io.Closer
	_ = closer
}
