// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockExecutor struct {
	mu          sync.Mutex
	Result      tools.ToolResult
	Err         error
	Panic       bool
	Delay       time.Duration
	Called      bool
	BlockCh     chan struct{}
	ExecuteHook func()
}

func (m *mockExecutor) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall, hb chan<- struct{}) (tools.ToolResult, error) {
	m.mu.Lock()
	m.Called = true
	m.mu.Unlock()

	if m.ExecuteHook != nil {
		m.ExecuteHook()
	}

	if m.Delay > 0 {
		select {
		case <-m.BlockCh:
		case <-ctx.Done():
			return tools.ToolResult{}, ctx.Err()
		}
	}
	if m.Panic {
		panic("mock panic")
	}
	return m.Result, m.Err
}

type mockToolAuthService struct {
	AuthorizeFunc func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error
}

func (m *mockToolAuthService) Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
	if m.AuthorizeFunc != nil {
		return m.AuthorizeFunc(ctx, tool, call)
	}
	return nil
}

type mockEventBus struct {
	events.EventBus
	mu        sync.Mutex
	Published []events.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Published = append(m.Published, e)
	return nil
}

func (m *mockEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (m *mockEventBus) Shutdown(ctx context.Context) error                { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error                   { return nil }
func (m *mockEventBus) Listen(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
func (m *mockEventBus) WaitStarted() {}

type mockSecurityManager struct {
	domain_security.Manager
	AllowedCommands map[string]bool
	AllowAll        bool
}

func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	if m.AllowAll {
		return true
	}
	return m.AllowedCommands[command]
}

func (m *mockSecurityManager) Close() error { return nil }

type mockConsentSecurityManager struct {
	domain_security.Manager
	ConfirmResult bool
}

func (m *mockConsentSecurityManager) IsBypassActive() bool { return false }
func (m *mockConsentSecurityManager) TerminalLock()        {}
func (m *mockConsentSecurityManager) TerminalUnlock()      {}
func (m *mockConsentSecurityManager) Confirm(ctx context.Context, msg string) (bool, error) {
	return m.ConfirmResult, nil
}

func (m *mockConsentSecurityManager) Close() error { return nil }

type panicRegistry struct {
	tools.Registry
	PanicOnExec bool
	PanicOnGet  bool
	Serial      bool
}

func (r *panicRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if r.PanicOnGet {
		panic("registry GetDeclarations panic")
	}
	return []*tools.ToolDeclaration{{Name: "any"}}
}

func (r *panicRegistry) IsSerial(name string) bool {
	return r.Serial
}

func (r *panicRegistry) IsLongRunning(name string) bool {
	return false
}

func (r *panicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if r.PanicOnExec {
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
