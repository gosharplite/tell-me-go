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
