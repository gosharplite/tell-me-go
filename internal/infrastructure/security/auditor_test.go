// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditor_LogAudit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "audit.log")
	a := newAuditor()
	a.SetLogFile(logFile)

	a.LogAudit("TEST_ACTION", "Action", "Test", "Detail", "Something")

	// Must close to flush and release lock on Windows
	a.Close()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "AUDIT: TEST_ACTION") {
		t.Error("Log missing AUDIT: TEST_ACTION")
	}
	if !strings.Contains(content, "Action=Test") {
		t.Error("Log missing Action=Test")
	}
	if !strings.Contains(content, "Detail=Something") {
		t.Error("Log missing Detail=Something")
	}
}

func TestAuditor_NoLogFile(t *testing.T) {
	t.Parallel()
	a := newAuditor()
	// Should not panic or fail when logFile is empty
	a.LogAudit("TEST", "Action", "Test")
}

func TestAuditor_SetLogFileError(t *testing.T) {
	t.Parallel()
	mock := &MockInteractor{}
	a := newAuditor()
	a.SetInteractor(mock)

	// Try to open a path that shouldn't be writable or is a directory
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "nonexistent_dir", "audit.log")

	a.SetLogFile(invalidPath)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.Warns) == 0 {
		t.Error("Expected warning message, got none")
	} else if !strings.Contains(mock.Warns[0], "Failed to open command log file") {
		t.Errorf("Unexpected warning message: %s", mock.Warns[0])
	}
}

func TestAuditor_Close(t *testing.T) {
	t.Run("closes open file successfully", func(t *testing.T) {
		a := newAuditor()

		// Use t.TempDir() for guaranteed isolated cleanup
		logFile := filepath.Join(t.TempDir(), "audit.log")
		a.SetLogFile(logFile)

		if err := a.Close(); err != nil {
			t.Fatalf("unexpected error closing auditor: %v", err)
		}

		// Verify internal state is explicitly cleared
		if a.file != nil || a.logger != nil {
			t.Errorf("expected file and logger to be nil after Close()")
		}
	})

	t.Run("safe to call on uninitialized file", func(t *testing.T) {
		a := newAuditor()
		if err := a.Close(); err != nil {
			t.Fatalf("expected nil error when closing uninitialized auditor, got %v", err)
		}
	})
}
