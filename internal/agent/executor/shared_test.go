// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockSecurityManager struct {
	domain_security.Manager
	allowedCommands map[string]bool
	allowAll        bool
}

func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	if m.allowAll {
		return true
	}
	return m.allowedCommands[command]
}

func (m *mockSecurityManager) Close() error { return nil }

type mockConsentSecurityManager struct {
	domain_security.Manager
	confirmResult bool
}

func (m *mockConsentSecurityManager) IsBypassActive() bool { return false }
func (m *mockConsentSecurityManager) TerminalLock()        {}
func (m *mockConsentSecurityManager) TerminalUnlock()      {}
func (m *mockConsentSecurityManager) Confirm(ctx context.Context, msg string) (bool, error) {
	return m.confirmResult, nil
}

func (m *mockConsentSecurityManager) Close() error { return nil }

type panicRegistry struct {
	tools.Registry
	panicOnExec bool
	panicOnGet  bool
	serial      bool
}

func (r *panicRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if r.panicOnGet {
		panic("registry GetDeclarations panic")
	}
	return []*tools.ToolDeclaration{{Name: "any"}}
}

func (r *panicRegistry) IsSerial(name string) bool {
	return r.serial
}

func (r *panicRegistry) IsLongRunning(name string) bool {
	return false
}

func (r *panicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if r.panicOnExec {
		panic("registry Execute panic")
	}
	return tools.ToolResult{}, nil
}

func (r *panicRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{}
}

func (r *panicRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	panic("not implemented")
}

func (r *panicRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	panic("not implemented")
}

func (r *panicRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	panic("not implemented")
}

func (r *panicRegistry) ListAvailableToolkits() []string {
	panic("not implemented")
}
