// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysistest

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// MockSecurityProvider is a configurable mock of domain_security.Manager for use in tests.
//
// Set TempDir to allow paths within that directory (IsPathSafe resolves relative paths
// and checks the prefix). Set DenyAll to true to simulate a denying security provider
// that rejects all path access.
type MockSecurityProvider struct {
	TempDir string
	DenyAll bool
}

func (m *MockSecurityProvider) IsPathSafe(path string) (string, error) {
	if m.DenyAll {
		return "", fmt.Errorf("path not authorized")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if m.TempDir != "" && !strings.HasPrefix(abs, m.TempDir) {
		return "", fmt.Errorf("path out of bounds")
	}
	return abs, nil
}

func (m *MockSecurityProvider) IsPathWritable(path string) (string, error) {
	return m.IsPathSafe(path)
}

func (m *MockSecurityProvider) TerminalLock()   {}
func (m *MockSecurityProvider) TerminalUnlock() {}
func (m *MockSecurityProvider) IsBypassActive() bool {
	return false
}
func (m *MockSecurityProvider) IsCommandAllowed(command string) bool {
	return true
}
func (m *MockSecurityProvider) Prompt(message string) {}
func (m *MockSecurityProvider) Warn(message string)   {}
func (m *MockSecurityProvider) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *MockSecurityProvider) LogAudit(action string, args ...any) {
}
func (m *MockSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (m *MockSecurityProvider) ReadLine(ctx context.Context) (string, error) {
	return "", nil
}
func (m *MockSecurityProvider) Close() error { return nil }
