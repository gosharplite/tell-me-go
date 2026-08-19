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
	sm := NewSecurityManager(func() domain.UserInteractor { return &mockInteractor{Answer: "y"} })

	// Default
	if sm.IsBypassActive() {
		t.Error("Expected bypass to be inactive by default")
	}

	// Set active
	sm.SetBypassActive(true)
	if !sm.IsBypassActive() {
		t.Error("Expected bypass to be active")
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

func TestSecurityManager_IsToolAllowed(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	tests := []struct {
		name string
		want bool
	}{
		{"mcp_github_list_issues", true},
		{"mcp_github_run_secret_scanning", true},
		{"read_files", true},
		{"unknown_tool", false},
		{"mcp", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sm.IsToolAllowed(tt.name); got != tt.want {
				t.Errorf("IsToolAllowed(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}

	// Exact-gate pin: IsCommandAllowed must remain exact-match and must NOT
	// inherit the mcp_ prefix allowance granted by IsToolAllowed.
	if sm.IsCommandAllowed("mcp_github_list_issues") {
		t.Error("IsCommandAllowed must remain exact-match (no prefix allowance)")
	}
}

func assertAuthorization(t *testing.T, name string, ok bool, err error, expectedOk bool) {
	t.Helper()
	if (err != nil) || (ok != expectedOk) {
		t.Errorf("%s: Authorize() = %v, %v; want %v, nil", name, ok, err, expectedOk)
	}
}

func TestSecurityManager_Authorize(t *testing.T) {
	t.Parallel()
	// 1. Authorize when isSafe=true

	var mi domain.UserInteractor
	sm := NewSecurityManager(func() domain.UserInteractor { return mi })
	ok, err := sm.Authorize(context.Background(), "label", "detail", "reason", true)
	assertAuthorization(t, "Authorize(isSafe=true)", ok, err, true)

	// 2. Authorize when bypass=true
	sm.SetBypassActive(true)
	ok, err = sm.Authorize(context.Background(), "label", "detail", "reason", false)
	assertAuthorization(t, "Authorize(bypass=true)", ok, err, true)

	// 3. Authorize with user interaction (Yes)
	sm.SetBypassActive(false)
	mi = &mockInteractor{Answer: "y"}
	ok, err = sm.Authorize(context.Background(), "label", "detail", "reason", false)
	assertAuthorization(t, "Authorize(user=y)", ok, err, true)

	// 4. Authorize with user interaction (No)
	mi = &mockInteractor{Answer: "n"}
	ok, err = sm.Authorize(context.Background(), "label", "detail", "reason", false)
	assertAuthorization(t, "Authorize(user=n)", ok, err, false)

	// 5. Authorize with context-based approval
	ctxApproved := domain.WithApprovedTools(context.Background(), []string{"test_tool"})
	ctxApproved = domain.WithCurrentTool(ctxApproved, "test_tool")
	ok, err = sm.Authorize(ctxApproved, "label", "detail", "reason", false)
	assertAuthorization(t, "Authorize(ctx_approved=true)", ok, err, true)
}

func TestSecurityManager_PathManagement(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)

	tmpDir := t.TempDir()
	safePath := filepath.Join(tmpDir, "safe")
	roPath := filepath.Join(tmpDir, "readonly")

	sm.RegisterSafePath(safePath)
	sm.RegisterReadOnlyPath(roPath)

	if !contains(sm.GetSafePaths(), safePath) {
		t.Errorf("Expected %s in safe paths", safePath)
	}
	if !contains(sm.getReadOnlyPaths(), roPath) {
		t.Errorf("Expected %s in read-only paths", roPath)
	}

	_ = sm.removeSafePath(safePath)
	if contains(sm.GetSafePaths(), safePath) {
		t.Errorf("Did not expect %s in safe paths after removal", safePath)
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if strings.Contains(s, val) || strings.Contains(filepath.ToSlash(s), filepath.ToSlash(val)) {
			return true
		}
	}
	return false
}

func TestSecurityManager_Misc(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(func() domain.UserInteractor { return &mockInteractor{Answer: "y"} })
	defer func() {
		_ = sm.Close()
	}()

	// getPolicy / setPolicy
	p := sm.getPolicy()
	if p == nil {
		t.Error("getPolicy returned nil")
	}
	sm.setPolicy(p)

	// IsPathWritable
	_, _ = sm.IsPathWritable(filepath.Join(t.TempDir(), "test"))

	// LogAudit / SetCommandsLogFile
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "commands.log")
	sm.SetCommandsLogFile(logFile)
	sm.LogAudit("TEST_ACTION", "ACTION", "test", "DETAIL", "detail")

	// Ensure file is closed before reading if needed, but auditor does it automatically or we close it
	_ = sm.Close()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logContent := string(data)
	if !strings.Contains(logContent, "AUDIT: TEST_ACTION") || !strings.Contains(logContent, "ACTION=test") {
		t.Errorf("Audit log content mismatch: %q", logContent)
	}

	roPath := filepath.Join(t.TempDir(), "ro")
	sm.RegisterReadOnlyPath(roPath)
	_ = sm.removeReadOnlyPath(roPath)

	// Interactor methods
	if sm.getInteractor() == nil {
		t.Error("getInteractor returned nil")
	}

	sm.TerminalLock()
	sm.TerminalUnlock()

	_, _ = sm.readSingleKey(context.Background())
}

func TestSecurityManager_Prompt(t *testing.T) {
	t.Parallel()
	mi := &mockInteractor{Answer: "y"}
	sm := NewSecurityManager(func() domain.UserInteractor { return mi })
	sm.Prompt("test prompt message")

	if len(mi.Warns) == 0 || !strings.Contains(mi.Warns[0], "test prompt message") {
		t.Errorf("expected Prompt to delegate to interactor, got warns: %v", mi.Warns)
	}
}

// TestSecurityManager_RegisterEmptyPath_NoOp covers the empty-path guards in
// RegisterSafePath (manager.go:132-134) and RegisterReadOnlyPath
// (manager.go:157-159): registering "" must be a no-op that leaves the
// respective path set unchanged.
func TestSecurityManager_RegisterEmptyPath_NoOp(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)

	// RegisterSafePath("") must be a no-op: no panic, no registered paths.
	sm.RegisterSafePath("")
	if got := sm.GetSafePaths(); len(got) != 0 {
		t.Errorf("RegisterSafePath(\"\") registered paths: %v", got)
	}

	// RegisterReadOnlyPath("") must be a no-op.
	sm.RegisterReadOnlyPath("")
	if got := sm.getReadOnlyPaths(); len(got) != 0 {
		t.Errorf("RegisterReadOnlyPath(\"\") registered paths: %v", got)
	}

	// Complement: registering a real path in each mode works as expected.
	tmpDir := t.TempDir()
	safePath := filepath.Join(tmpDir, "safe")
	roPath := filepath.Join(tmpDir, "readonly")

	sm.RegisterSafePath(safePath)
	if !contains(sm.GetSafePaths(), safePath) {
		t.Errorf("expected %s in safe paths after RegisterSafePath", safePath)
	}

	sm.RegisterReadOnlyPath(roPath)
	if !contains(sm.getReadOnlyPaths(), roPath) {
		t.Errorf("expected %s in read-only paths after RegisterReadOnlyPath", roPath)
	}
}
