// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

func TestSecurityManager_Bypass(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			if got := sm.IsCommandAllowed(tt.cmd); got != tt.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestSecurityManager_Authorize(t *testing.T) {
	t.Parallel()
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

	// 5. Authorize with context-based approval
	ctxApproved := domain.WithApproval(context.Background(), true)
	ok, err = sm.Authorize(ctxApproved, "label", "detail", "reason", false)
	if err != nil || !ok {
		t.Errorf("Authorize(ctx_approved=true) = %v, %v; want true, nil", ok, err)
	}
}

func TestSecurityManager_PathManagement(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	sm := NewSecurityManager(&MockInteractor{Answer: "y"})

	// getPolicy / setPolicy
	p := sm.getPolicy()
	if p == nil {
		t.Error("getPolicy returned nil")
	}
	sm.setPolicy(p)

	// IsPathWritable
	_, _ = sm.IsPathWritable("/tmp/test")

	// confirmDestructiveAction
	ok, err := sm.confirmDestructiveAction(context.Background(), "delete", "file", "detail")
	if err != nil || !ok {
		t.Errorf("confirmDestructiveAction failed: %v, %v", err, ok)
	}

	// LogAudit / SetCommandsLogFile
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "commands.log")
	sm.SetCommandsLogFile(logFile)
	sm.LogAudit("TEST_ACTION", "ACTION", "test", "DETAIL", "detail")

	data, _ := os.ReadFile(logFile)
	logContent := string(data)
	if !strings.Contains(logContent, "AUDIT: TEST_ACTION") || !strings.Contains(logContent, "ACTION=test") {
		t.Errorf("Audit log content mismatch: %q", logContent)
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

func TestSecurityManager_Confirm_Bypass(t *testing.T) {
	t.Parallel()
	// Default behavior (no bypass) - user says No
	interactor := &MockInteractor{Answer: "n"}
	sm := NewSecurityManager(interactor)
	ok, err := sm.Confirm(context.Background(), "Should I?")
	if err != nil || ok {
		t.Errorf("Confirm(user=n, bypass=false) = %v, %v; want false, nil", ok, err)
	}

	// Bypass active - should be auto-approved even if interactor would say No
	sm.SetBypassActive(true)
	ok, err = sm.Confirm(context.Background(), "Should I?")
	if err != nil || !ok {
		t.Errorf("Confirm(user=n, bypass=true) = %v, %v; want true, nil", ok, err)
	}

	// Verify that a warning was captured
	if len(interactor.Warns) == 0 || !strings.Contains(interactor.Warns[0], "[Auto-Approved]") {
		t.Errorf("Expected auto-approval warning, got: %v", interactor.Warns)
	}

	// Context approved - should be auto-approved even if bypass is inactive and interactor would say No
	sm.SetBypassActive(false)
	ctxApproved := domain.WithApproval(context.Background(), true)
	ok, err = sm.Confirm(ctxApproved, "Should I?")
	if err != nil || !ok {
		t.Errorf("Confirm(user=n, ctx_approved=true) = %v, %v; want true, nil", ok, err)
	}
}
