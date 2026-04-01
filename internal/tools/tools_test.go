// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockSessionProvider struct {
	mock.Mock
}

func (m *mockSessionProvider) GetInfo() ports.SessionInfo {
	return m.Called().Get(0).(ports.SessionInfo)
}
func (m *mockSessionProvider) SetInfo(info ports.SessionInfo) { m.Called(info) }
func (m *mockSessionProvider) Close() error                   { return m.Called().Error(0) }
func (m *mockSessionProvider) GetTasks() ports.TaskStore      { return nil }
func (m *mockSessionProvider) GetSettings() ports.KVStore     { return nil }

func TestNewToolRegistry(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	if err := tools.RegisterAll(tools.ToolRegistrationParams{
		Registry:        r,
		SecurityManager: sm,
		LogFile:         "tokens.log",
		Model:           "model",
		Mode:            "mode",
		AssetsDir:       t.TempDir(),
		FileSystem:      infrapersistence.NewOSFileSystem(),
	}); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	if len(r.GetDeclarations()) == 0 {
		t.Error("expected registered tools, got none")
	}
}

func TestRegisterAll_WithSessionProvider(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	sp := &mockSessionProvider{}
	if err := tools.RegisterAll(tools.ToolRegistrationParams{
		Registry:        r,
		SecurityManager: sm,
		SessionProvider: sp,
		LogFile:         "tokens.log",
		Model:           "model",
		Mode:            "mode",
		AssetsDir:       t.TempDir(),
		FileSystem:      infrapersistence.NewOSFileSystem(),
	}); err != nil {
		t.Fatalf("RegisterAll with SessionProvider failed: %v", err)
	}
}

func TestToolExecution(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	if err := tools.RegisterAll(tools.ToolRegistrationParams{
		Registry:        r,
		SecurityManager: sm,
		LogFile:         "tokens.log",
		Model:           "model",
		Mode:            "mode",
		AssetsDir:       t.TempDir(),
		FileSystem:      infrapersistence.NewOSFileSystem(),
	}); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	ctx := context.Background()
	// list_files is registered by workspace.Register
	_, err := r.Execute(ctx, "list_files", map[string]interface{}{"path": "."}, nil)
	if err != nil {
		t.Errorf("failed to execute list_files: %v", err)
	}
}

func TestGenerateMermaidDiagram(t *testing.T) {
	t.Parallel()
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	if err := tools.RegisterAll(tools.ToolRegistrationParams{
		Registry:        r,
		SecurityManager: sm,
		LogFile:         "tokens.log",
		Model:           "model",
		Mode:            "mode",
		AssetsDir:       t.TempDir(),
		FileSystem:      infrapersistence.NewOSFileSystem(),
	}); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	ctx := context.Background()
	graph := map[string]interface{}{
		"pkg1": []interface{}{"pkg2", "pkg3"},
		"pkg2": []string{"pkg3"},
	}

	res, err := r.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{"graph": graph}, nil)
	if err != nil {
		t.Fatalf("failed to execute generate_mermaid_diagram: %v", err)
	}

	if res.Text == "" {
		t.Error("expected mermaid output, got empty string")
	}

	// Test invalid graph
	res, err = r.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{"graph": 123}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error") {
		t.Errorf("expected error message in result, got %s", res.Text)
	}

	// Test missing graph
	res, err = r.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error") {
		t.Errorf("expected error message in result, got %s", res.Text)
	}
}

// MockRegistry forces an error on registration after a certain number of successful calls.
type MockRegistry struct {
	failAfter int
	count     int
}

func (m *MockRegistry) Register(def *domaintools.ToolDeclaration, fn domaintools.ToolFunc) error {
	if m.count >= m.failAfter {
		return errors.New("simulated registration failure")
	}
	m.count++
	return nil
}

func (m *MockRegistry) RegisterWithOptions(def *domaintools.ToolDeclaration, fn domaintools.ToolFunc, opts domaintools.ToolOptions) error {
	if m.count >= m.failAfter {
		return errors.New("simulated registration failure")
	}
	m.count++
	return nil
}

func (m *MockRegistry) GetDeclarations() []*domaintools.ToolDeclaration { return nil }

func (m *MockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (domaintools.ToolResult, error) {
	return domaintools.ToolResult{}, errors.New("not implemented")
}

func (m *MockRegistry) IsSerial(name string) bool      { return false }
func (m *MockRegistry) IsLongRunning(name string) bool { return false }

func TestRegisterAll_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		failAfter int
		setup     func(*tools.ToolRegistrationParams)
	}{
		{
			name:      "fail in workspace.Register",
			failAfter: 0,
		},
		{
			name:      "fail in workspace.RegisterPersistence",
			failAfter: 21,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &mockSessionProvider{}
			},
		},
		{
			name:      "fail in analysis.Register",
			failAfter: 25,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &mockSessionProvider{}
			},
		},
		{
			name:      "fail in developer.Register",
			failAfter: 46,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &mockSessionProvider{}
			},
		},
		{
			name:      "fail in integrations.RegisterAll",
			failAfter: 53,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &mockSessionProvider{}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockReg := &MockRegistry{failAfter: tt.failAfter}
			params := tools.ToolRegistrationParams{
				Registry: mockReg,
			}
			if tt.setup != nil {
				tt.setup(&params)
			}

			err := tools.RegisterAll(params)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "simulated registration failure")
		})
	}
}

func (m *MockRegistry) GetOptions(name string) domaintools.ToolOptions {
	return domaintools.ToolOptions{}
}

func (m *MockRegistry) RegisterToToolkit(toolkit string, def *domaintools.ToolDeclaration, handler domaintools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *MockRegistry) RegisterToToolkitWithOptions(toolkit string, def *domaintools.ToolDeclaration, handler domaintools.ToolFunc, opts domaintools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func (m *MockRegistry) GetCoreDeclarations() []*domaintools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *MockRegistry) GetDeclarationsByToolkits(toolkits []string) []*domaintools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *MockRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}
