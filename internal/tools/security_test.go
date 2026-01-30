package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPathSafe_SymlinkRace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "security_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	safeDir := filepath.Join(tempDir, "safe")
	err = os.Mkdir(safeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create safe dir: %v", err)
	}

	// Use a directory that is definitely not in Temp or CWD.
	// /etc is usually safe to use as a target for a symlink in a test.
	externalDir := "/etc"

	sm := NewSecurityManager()
	sm.RegisterSafePath(safeDir)

	// Create a symlink in safeDir pointing to externalDir
	linkPath := filepath.Join(safeDir, "link")
	err = os.Symlink(externalDir, linkPath)
	if err != nil {
		// If we can't create a symlink, skip the test
		t.Skipf("failed to create symlink (might not have permissions): %v", err)
	}

	// Target a non-existent file inside the symlinked external directory
	targetPath := filepath.Join(linkPath, "newfile-that-does-not-exist")

	// Current implementation should fail this test if vulnerable
	err = sm.IsPathSafe(targetPath)
	if err == nil {
		t.Errorf("IsPathSafe should have failed for path through symlink to external directory: %s", targetPath)
	}
}

func TestIsPathSafe_ValidPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "security_test_valid")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	safeDir := filepath.Join(tempDir, "safe")
	err = os.Mkdir(safeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create safe dir: %v", err)
	}

	sm := NewSecurityManager()
	sm.RegisterSafePath(safeDir)

	targetPath := filepath.Join(safeDir, "newfile")
	err = sm.IsPathSafe(targetPath)
	if err != nil {
		t.Errorf("IsPathSafe should have allowed valid path: %v", err)
	}
}
