// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityManager_Bypass(t *testing.T) {
	tmpDir := t.TempDir()
	bypassFile := filepath.Join(tmpDir, "bypass")
	sm := NewSecurityManager(&MockInteractor{Answer: "y"})
	sm.SetBypassFile(bypassFile)

	// Default
	if sm.IsBypassActive() {
		t.Error("Expected bypass to be inactive by default")
	}

	// Set active
	sm.SetBypassActive(true)
	if !sm.IsBypassActive() {
		t.Error("Expected bypass to be active")
	}

	// Save
	sm.saveBypassState(context.Background())
	data, err := os.ReadFile(bypassFile)
	if err != nil {
		t.Fatalf("Failed to read bypass file: %v", err)
	}
	if string(data) != "true" {
		t.Errorf("Expected 'true' in bypass file, got %q", string(data))
	}

	// Load
	sm2 := NewSecurityManager(nil)
	sm2.SetBypassFile(bypassFile)
	sm2.LoadBypassState()
	if !sm2.IsBypassActive() {
		t.Error("Expected sm2 to have bypass active after loading")
	}
}

func TestSecurityManager_IsCommandAllowed(t *testing.T) {
	sm := NewSecurityManager(nil)
	tests := []struct {
		cmd  string
		want bool
	}{
		{"go", true},
		{"ls", true},
		{"rm", true},
		{"malicious_cmd", false},
		{"write_file", true},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := sm.IsCommandAllowed(tt.cmd); got != tt.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestSecurityManager_Authorize(t *testing.T) {
	// 1. Authorize when isSafe=true
	sm := NewSecurityManager(nil)
	ok, err := sm.Authorize(context.Background(), "label", "detail", "reason", true)
	if err != nil || !ok {
		t.Errorf("Authorize(isSafe=true) = %v, %v; want true, nil", ok, err)
	}

	// 2. Authorize when bypass=true
	sm.SetBypassActive(true)
	ok, err = sm.Authorize(context.Background(), "label", "detail", "reason", false)
	if err != nil || !ok {
		t.Errorf("Authorize(bypass=true) = %v, %v; want true, nil", ok, err)
	}

	// 3. Authorize with user interaction (Yes)
	sm.SetBypassActive(false)
	sm.SetInteractor(&MockInteractor{Answer: "y"})
	ok, err = sm.Authorize(context.Background(), "label", "detail", "reason", false)
	if err != nil || !ok {
		t.Errorf("Authorize(user=y) = %v, %v; want true, nil", ok, err)
	}

	// 4. Authorize with user interaction (No)
	sm.SetInteractor(&MockInteractor{Answer: "n"})
	ok, err = sm.Authorize(context.Background(), "label", "detail", "reason", false)
	if err != nil || ok {
		t.Errorf("Authorize(user=n) = %v, %v; want false, nil", ok, err)
	}
}

func TestSecurityManager_PathManagement(t *testing.T) {
	sm := NewSecurityManager(nil)
	sm.RegisterSafePath("/tmp/safe")
	sm.RegisterReadOnlyPath("/tmp/readonly")

	if !contains(sm.GetSafePaths(), "/tmp/safe") {
		t.Error("Expected /tmp/safe in safe paths")
	}
	if !contains(sm.getReadOnlyPaths(), "/tmp/readonly") {
		t.Error("Expected /tmp/readonly in read-only paths")
	}

	_ = sm.removeSafePath("/tmp/safe")
	if contains(sm.GetSafePaths(), "/tmp/safe") {
		t.Error("Did not expect /tmp/safe in safe paths after removal")
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if strings.Contains(s, val) { // Using Contains because Registered paths might be absolute
			return true
		}
	}
	return false
}

func TestSecurityManager_Misc(t *testing.T) {
	sm := NewSecurityManager(&MockInteractor{Answer: "y"})
	
	// getPolicy / setPolicy
	p := sm.getPolicy()
	if p == nil {
		t.Error("getPolicy returned nil")
	}
	sm.setPolicy(p)

	// IsPathWritable
	_, _ = sm.IsPathWritable("/tmp/test")

	// ConfirmDestructiveAction
	ok, err := sm.ConfirmDestructiveAction(context.Background(), "delete", "file", "detail")
	if err != nil || !ok {
		t.Errorf("ConfirmDestructiveAction failed: %v, %v", err, ok)
	}

	// LogAudit / SetCommandsLogFile
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "commands.log")
	sm.SetCommandsLogFile(logFile)
	sm.LogAudit("ACTION", "test", "DETAIL", "detail")
	
	data, _ := os.ReadFile(logFile)
	if !strings.Contains(string(data), "ACTION: test") {
		t.Error("Audit log content mismatch")
	}

	// Read/Write paths
	sm.SetSafePathsFile(filepath.Join(tmpDir, "safe.json"))
	sm.SetReadOnlyPathsFile(filepath.Join(tmpDir, "readonly.json"))
	_ = sm.saveSafePaths(context.Background())
	_ = sm.saveReadOnlyPaths(context.Background())
	_ = sm.LoadSafePaths()
	_ = sm.LoadReadOnlyPaths()
	
	sm.RegisterReadOnlyPath("/tmp/ro")
	_ = sm.removeReadOnlyPath("/tmp/ro")

	// Interactor methods
	if sm.GetInteractor() == nil {
		t.Error("GetInteractor returned nil")
	}
	
	sm.TerminalLock()
	sm.TerminalUnlock()
	
	_, _ = sm.readSingleKey(context.Background())
	_, _ = sm.ReadLine(context.Background())
}
