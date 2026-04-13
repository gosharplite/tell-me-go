// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
)

// MockSecurityManager is a simple security manager for testing.
type MockSecurityManager struct {
	AllowAll        bool
	AllowedCommands map[string]bool
	BypassActive    bool
	AuthorizeFunc   func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
	IsSafeFunc      func(path string) (string, error)
	IsWritableFunc  func(path string) (string, error)
	Interactor      security.UserInteractor
}

var _ security.Manager = (*MockSecurityManager)(nil)

func (m *MockSecurityManager) IsPathSafe(path string) (string, error) {
	if m.AllowAll || m.BypassActive {
		return path, nil
	}
	if m.IsSafeFunc != nil {
		return m.IsSafeFunc(path)
	}
	return path, nil
}

func (m *MockSecurityManager) IsPathWritable(path string) (string, error) {
	if m.AllowAll || m.BypassActive {
		return path, nil
	}
	if m.IsWritableFunc != nil {
		return m.IsWritableFunc(path)
	}
	return path, nil
}

func (m *MockSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	if m.AuthorizeFunc != nil {
		return m.AuthorizeFunc(ctx, label, detail, reason, isSafe)
	}
	return m.AllowAll || m.BypassActive, nil
}

func (m *MockSecurityManager) LogAudit(action string, args ...any) {}
func (m *MockSecurityManager) Close() error                        { return nil }
func (m *MockSecurityManager) TerminalLock()                       {}
func (m *MockSecurityManager) TerminalUnlock()                     {}
func (m *MockSecurityManager) Prompt(message string)               {}
func (m *MockSecurityManager) Warn(message string)                 {}

func (m *MockSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	if m.Interactor != nil {
		return m.Interactor.Confirm(ctx, message)
	}
	return true, nil
}

func (m *MockSecurityManager) ReadLine(ctx context.Context) (string, error) {
	if m.Interactor != nil {
		return m.Interactor.ReadLine(ctx)
	}
	return "", nil
}

func (m *MockSecurityManager) IsCommandAllowed(command string) bool {
	if m.AllowAll || m.BypassActive {
		return true
	}
	if m.AllowedCommands != nil {
		return m.AllowedCommands[command]
	}
	return false
}

func (m *MockSecurityManager) IsBypassActive() bool {
	return m.BypassActive
}

func (m *MockSecurityManager) SetBypassActive(active bool) {
	m.BypassActive = active
}

func (m *MockSecurityManager) SetInteractor(i security.UserInteractor) {
	m.Interactor = i
}

func (m *MockSecurityManager) RegisterSafePath(path string) {}

// MockInteractor is a simple user interactor for testing.
type MockInteractor struct {
	Answer  string
	Warns   []string
	Prompts []string
	Err     error
}

func (m *MockInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	return m.Answer == "y" || m.Answer == "yes", nil
}

func (m *MockInteractor) Warn(message string) {
	m.Warns = append(m.Warns, message)
}

func (m *MockInteractor) Prompt(message string) {
	m.Prompts = append(m.Prompts, message)
}

func (m *MockInteractor) ReadSingleKey(ctx context.Context) (string, error) {
	return m.Answer, m.Err
}

func (m *MockInteractor) ReadLine(ctx context.Context) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	if m.Answer == "" {
		return "", io.EOF
	}
	return m.Answer, nil
}

// MockCommandValidator is a mock implementation of security.CommandValidator.
type MockCommandValidator struct {
	IsSafeFunc            func(command string) (bool, string)
	SplitFunc             func(cmd string) ([]string, error)
	ValidateStructureFunc func(parts []string) error
	CheckPathSafetyFunc   func(parts []string) (bool, string)
	HasShellFeaturesFunc  func(parts []string) bool
}

func (m *MockCommandValidator) IsSafe(command string) (bool, string) {
	if m.IsSafeFunc != nil {
		return m.IsSafeFunc(command)
	}
	return true, ""
}

func (m *MockCommandValidator) Split(cmd string) ([]string, error) {
	if m.SplitFunc != nil {
		return m.SplitFunc(cmd)
	}
	// Simple split for mock, but detect unclosed quotes for tests
	if strings.Count(cmd, "'")%2 != 0 || strings.Count(cmd, "\"")%2 != 0 {
		return nil, errors.New("unclosed quote")
	}
	return strings.Fields(cmd), nil
}

func (m *MockCommandValidator) ValidateStructure(parts []string) error {
	if m.ValidateStructureFunc != nil {
		return m.ValidateStructureFunc(parts)
	}
	return nil
}

func (m *MockCommandValidator) CheckPathSafety(parts []string) (bool, string) {
	if m.CheckPathSafetyFunc != nil {
		return m.CheckPathSafetyFunc(parts)
	}
	return true, ""
}

func (m *MockCommandValidator) HasShellFeatures(parts []string) bool {
	if m.HasShellFeaturesFunc != nil {
		return m.HasShellFeaturesFunc(parts)
	}
	// Default heuristic for PowerShell cmdlets
	for _, p := range parts {
		if (len(p) > 3 && p[0] >= 'A' && p[0] <= 'Z' && containsHyphen(p)) ||
			(len(p) > 1 && p[0] == '$') {
			return true
		}
	}
	return false
}

func containsHyphen(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			return true
		}
	}
	return false
}
