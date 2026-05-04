// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"testing"
)

// newPathPolicyForTest returns a pathPolicy for testing.
func newPathPolicyForTest(t *testing.T) *pathPolicy {
	t.Helper()
	return newPathPolicy(nil)
}

func TestIsSystemDirectory_Exemptions(t *testing.T) {
	p := newPathPolicyForTest(t)

	// CWD must always be allowed.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}
	if err := p.isSystemDirectory(cwd); err != nil {
		t.Errorf("expected CWD to be allowed, got error: %v", err)
	}

	// Temp dir must always be allowed.
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, "test.txt")
	absTempFile, err := filepath.Abs(tempFile)
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}
	if err := p.isSystemDirectory(absTempFile); err != nil {
		t.Errorf("expected temp file to be allowed, got error: %v", err)
	}
}
