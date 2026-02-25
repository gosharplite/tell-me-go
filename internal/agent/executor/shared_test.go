// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockSecurityManager struct {
	domain_security.ISecurityManager
	allowedCommands map[string]bool
	allowAll        bool
}

func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	if m.allowAll {
		return true
	}
	return m.allowedCommands[command]
}

type mockConsentSecurityManager struct {
	domain_security.ISecurityManager
	confirmResult bool
}

func (m *mockConsentSecurityManager) IsBypassActive() bool { return false }
func (m *mockConsentSecurityManager) TerminalLock()        {}
func (m *mockConsentSecurityManager) TerminalUnlock()      {}
func (m *mockConsentSecurityManager) Confirm(ctx context.Context, msg string) (bool, error) {
	return m.confirmResult, nil
}

type mockStrategy struct{}

func (s *mockStrategy) Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part {
	name := ""
	if call != nil {
		name = call.Name
	}
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			ID:       call.ID,
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

type panicRegistry struct {
	tools.IToolRegistry
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

func (r *panicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	if r.panicOnExec {
		panic("registry Execute panic")
	}
	return tools.ToolResult{}, nil
}
