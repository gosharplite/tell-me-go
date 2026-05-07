// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
)

// MockSecurityManager is a permissive test double for security.Manager.
// When AllowAll or BypassActive is true it short-circuits all path,
// command, and Authorize checks to allow-with-no-prompt. Test authors
// can override individual decisions via AuthorizeFunc, IsSafeFunc, and
// IsWritableFunc, and can plug in a UserInteractor to drive Confirm /
// ReadLine flows.
type MockSecurityManager struct {
	AllowAll        bool
	AllowedCommands map[string]bool
	BypassActive    bool
	AuthorizeFunc   func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
	IsSafeFunc      func(path string) (string, error)
	IsWritableFunc  func(path string) (string, error)
	ConfirmFunc     func(ctx context.Context, message string) (bool, error)
	ConfirmCalled   bool
	LastConfirmText string
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
	m.ConfirmCalled = true
	m.LastConfirmText = message
	if m.ConfirmFunc != nil {
		return m.ConfirmFunc(ctx, message)
	}
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

func (m *MockSecurityManager) RegisterSafePath(path string) {}
