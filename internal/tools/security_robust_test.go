// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityManagerRobust(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	
	bypassFile := filepath.Join(tmpDir, "bypass.json")
	safePathsFile := filepath.Join(tmpDir, "safepaths.json")
	readOnlyPathsFile := filepath.Join(tmpDir, "readpaths.json")
	logFile := filepath.Join(tmpDir, "audit.log")

	sm.SetBypassFile(bypassFile)
	sm.SetSafePathsFile(safePathsFile)
	sm.SetReadOnlyPathsFile(readOnlyPathsFile)
	sm.SetCommandsLogFile(logFile)

	t.Run("Persistence_Bypass", func(t *testing.T) {
		// Toggle bypass (assuming we can toggle it, usually via tools, 
		// but let's check if there's a direct way or if we just test Load/Save)
		// For now, testing Load/Save logic
		sm.SaveBypassState()
		if _, err := os.Stat(bypassFile); err != nil {
			t.Errorf("Bypass file not created: %v", err)
		}
		sm.LoadBypassState()
	})

	t.Run("Persistence_Paths", func(t *testing.T) {
		sm.RegisterSafePath(tmpDir)
		if err := sm.SaveSafePaths(); err != nil {
			t.Fatalf("SaveSafePaths failed: %v", err)
		}
		
		newSm := NewSecurityManager()
		newSm.SetSafePathsFile(safePathsFile)
		if err := newSm.LoadSafePaths(); err != nil {
			t.Fatalf("LoadSafePaths failed: %v", err)
		}
		
		found := false
		for _, p := range newSm.GetSafePaths() {
			if p == tmpDir {
				found = true
				break
			}
		}
		if !found {
			t.Error("Safe path not loaded back")
		}

		sm.RegisterReadOnlyPath("/etc")
		sm.SaveReadOnlyPaths()
		newSm.SetReadOnlyPathsFile(readOnlyPathsFile)
		newSm.LoadReadOnlyPaths()
		
		foundRO := false
		for _, p := range newSm.GetReadOnlyPaths() {
			if p == "/etc" {
				foundRO = true
				break
			}
		}
		if !foundRO {
			t.Error("ReadOnly path not loaded back")
		}
	})

	t.Run("Path_Removal", func(t *testing.T) {
		p := filepath.Join(tmpDir, "toremove")
		sm.RegisterSafePath(p)
		if err := sm.RemoveSafePath(p); err != nil {
			t.Fatalf("RemoveSafePath failed: %v", err)
		}
		
		ro := "/tmp/ro"
		sm.RegisterReadOnlyPath(ro)
		if err := sm.RemoveReadOnlyPath(ro); err != nil {
			t.Fatalf("RemoveReadOnlyPath failed: %v", err)
		}
	})

	t.Run("IsPathSafe_Boundaries", func(t *testing.T) {
		// Test CWD (implicitly safe usually)
		cwd, _ := os.Getwd()
		if _, err := sm.IsPathSafe(cwd); err != nil {
			t.Errorf("CWD should be safe: %v", err)
		}

		// Test unregistered path
		if _, err := sm.IsPathSafe("/usr/bin"); err == nil {
			t.Error("Unregistered /usr/bin should NOT be safe")
		}

		// Test registered safe path
		sm.RegisterSafePath("/usr/local")
		if _, err := sm.IsPathSafe("/usr/local/bin"); err != nil {
			t.Errorf("Subpath of safe path should be safe: %v", err)
		}

		// Test registered read-only path
		sm.RegisterReadOnlyPath("/var/log")
		if _, err := sm.IsPathSafe("/var/log/syslog"); err != nil {
			t.Errorf("Subpath of read-only path should be safe: %v", err)
		}
		
		// Test writable vs read-only
		if _, err := sm.IsPathWritable("/var/log/syslog"); err == nil {
			t.Error("ReadOnly path should NOT be writable")
		}
	})

	t.Run("Audit_Logging", func(t *testing.T) {
		sm.logAudit("ACTION", "TEST", "TARGET", "FILE")
		content, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatalf("Failed to read audit log: %v", err)
		}
		if !strings.Contains(string(content), "ACTION: TEST") {
			t.Errorf("Audit log missing content: %s", string(content))
		}
	})
}
