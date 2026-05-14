// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

type mockToolRegistry struct {
	decls []*tools.ToolDeclaration
}

func (m *mockToolRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	m.decls = append(m.decls, def)
	return nil
}
func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.Register(def, handler)
}
func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (m *mockToolRegistry) IsSerial(name string) bool      { return false }
func (m *mockToolRegistry) IsLongRunning(name string) bool { return false }
func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.decls
}
func (m *mockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{}
}
func (m *mockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}
func (m *mockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}
func (m *mockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}
func (m *mockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}
func (m *mockToolRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}

func TestRegister(t *testing.T) {
	t.Parallel()
	registry := &mockToolRegistry{}
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	exec := &toolstest.MockExecutor{}
	validator := &toolstest.MockCommandValidator{}
	fs := persistencetest.NewPlainOSFileSystem()

	if err := Register(registry, sm, exec, validator, fs, infra_persistence.NewWorkspacePolicy(), nil); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	toolNames := []string{
		"list_files",
		"write_file",
		"search_files",
		"execute_command",
		"get_tree",
		"create_directory",
		"delete_path",
		"find_file",
		"replace_text",
		"append_text",
		"get_git_status",
		"get_git_diff",
		"get_git_log",
		"get_git_blame",
		"git_commit",
		"git_create_branch",
		"get_git_show",
		"get_file_diff",
		"undo_file_change",
		"pipe_commands",
	}

	for _, name := range toolNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			found := false
			for _, d := range registry.GetDeclarations() {
				if d.Name == name {
					found = true
					break
				}
			}
			assert.True(t, found, "tool %s should be registered", name)
		})
	}

	t.Run("check_system_health", func(t *testing.T) {
		reg := &mockToolRegistry{}
		mockHealth := &mockHealthCheckManager{}
		if err := Register(reg, sm, exec, validator, fs, infra_persistence.NewWorkspacePolicy(), mockHealth); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		found := false
		for _, d := range reg.GetDeclarations() {
			if d.Name == "check_system_health" {
				found = true
				break
			}
		}
		assert.True(t, found, "tool check_system_health should be registered when health manager is provided")
	})
}

// ---------------------------------------------------------------------------
// failingRegistry — mock that injects failures for a specific tool by name
// ---------------------------------------------------------------------------

type failingRegistry struct {
	*mockToolRegistry
	failOnTool string
	failErr    error
}

func (f *failingRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if def.Name == f.failOnTool {
		if f.failErr != nil {
			return f.failErr
		}
		return fmt.Errorf("injected failure for tool %s", def.Name)
	}
	return f.mockToolRegistry.Register(def, handler)
}

func (f *failingRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if def.Name == f.failOnTool {
		if f.failErr != nil {
			return f.failErr
		}
		return fmt.Errorf("injected failure for tool %s", def.Name)
	}
	return f.mockToolRegistry.RegisterWithOptions(def, handler, opts)
}

func (f *failingRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if def.Name == f.failOnTool {
		if f.failErr != nil {
			return f.failErr
		}
		return fmt.Errorf("injected failure for tool %s", def.Name)
	}
	return f.mockToolRegistry.RegisterToToolkit(toolkit, def, handler)
}

func (f *failingRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if def.Name == f.failOnTool {
		if f.failErr != nil {
			return f.failErr
		}
		return fmt.Errorf("injected failure for tool %s", def.Name)
	}
	return f.mockToolRegistry.RegisterToToolkitWithOptions(toolkit, def, handler, opts)
}

// ---------------------------------------------------------------------------
// Partial-failure tests
// ---------------------------------------------------------------------------

func TestRegisterFiles_PartialFailure(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	exec := &toolstest.MockExecutor{}
	fs := persistencetest.NewPlainOSFileSystem()
	wp := infra_persistence.NewWorkspacePolicy()

	t.Run("replace_text fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "replace_text",
		}
		err := registerFiles(registry, sm, fs, exec, wp)
		if err == nil {
			t.Fatal("expected error from registerFiles partial failure")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure' in error, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "replace_text") {
			t.Errorf("expected 'replace_text' in error, got %q", err.Error())
		}
	})

	t.Run("write_file fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "write_file",
		}
		err := registerFiles(registry, sm, fs, exec, wp)
		if err == nil {
			t.Fatal("expected error from registerFiles partial failure")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure' in error, got %q", err.Error())
		}
	})

	t.Run("list_files fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "list_files",
		}
		err := registerFiles(registry, sm, fs, exec, wp)
		if err == nil {
			t.Fatal("expected error from registerFiles partial failure")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure' in error, got %q", err.Error())
		}
	})
}

func TestRegisterSystem_PartialFailure(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}

	t.Run("execute_command fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "execute_command",
		}
		err := registerSystem(registry, sm, validator, nil)
		if err == nil {
			t.Fatal("expected error from registerSystem")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("pipe_commands fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "pipe_commands",
		}
		err := registerSystem(registry, sm, validator, nil)
		if err == nil {
			t.Fatal("expected error from registerSystem")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("ask_user fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "ask_user",
		}
		err := registerSystem(registry, sm, validator, nil)
		if err == nil {
			t.Fatal("expected error from registerSystem")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("check_system_health fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "check_system_health",
		}
		mockHealth := &mockHealthCheckManager{}
		err := registerSystem(registry, sm, validator, mockHealth)
		if err == nil {
			t.Fatal("expected error from registerSystem")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})
}

func TestRegisterGit_PartialFailure(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	exec := &toolstest.MockExecutor{}

	t.Run("first RegisterToToolkit fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "get_git_status",
		}
		err := registerGit(registry, sm, exec)
		if err == nil {
			t.Fatal("expected error from registerGit")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("mid RegisterToToolkit fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "get_git_blame",
		}
		err := registerGit(registry, sm, exec)
		if err == nil {
			t.Fatal("expected error from registerGit")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("RegisterToToolkitWithOptions fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "git_commit",
		}
		err := registerGit(registry, sm, exec)
		if err == nil {
			t.Fatal("expected error from registerGit")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("second RegisterToToolkit fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "get_git_diff",
		}
		err := registerGit(registry, sm, exec)
		if err == nil {
			t.Fatal("expected error from registerGit")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("last RegisterToToolkitWithOptions fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "git_create_branch",
		}
		err := registerGit(registry, sm, exec)
		if err == nil {
			t.Fatal("expected error from registerGit")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("third RegisterToToolkit fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "get_git_log",
		}
		err := registerGit(registry, sm, exec)
		if err == nil {
			t.Fatal("expected error from registerGit")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// Register top-level partial-failure tests
// ---------------------------------------------------------------------------

func TestRegister_PartialFailure(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	exec := &toolstest.MockExecutor{}
	validator := &toolstest.MockCommandValidator{}
	fs := persistencetest.NewPlainOSFileSystem()
	wp := infra_persistence.NewWorkspacePolicy()

	t.Run("registerSystem fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "execute_command",
		}
		err := Register(registry, sm, exec, validator, fs, wp, nil)
		if err == nil {
			t.Fatal("expected error from Register when registerSystem fails")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})

	t.Run("registerGit fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failOnTool:       "get_git_status",
		}
		err := Register(registry, sm, exec, validator, fs, wp, nil)
		if err == nil {
			t.Fatal("expected error from Register when registerGit fails")
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("expected 'injected failure', got %q", err.Error())
		}
	})
}

func TestRegister_SystemToolsOnly(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	reg := registry.New()
	// Register only system tools (skip git/files)
	err := registerSystem(reg, sm, security.NewCommandValidator(sm, nil), nil)
	if err != nil {
		t.Fatalf("registerSystem failed: %v", err)
	}
	decls := reg.GetDeclarations()
	// Should have execute_command, pipe_commands, ask_user (and check_system_health NOT, since health is nil)
	found := make(map[string]bool)
	for _, d := range decls {
		found[d.Name] = true
	}
	if !found["execute_command"] {
		t.Error("execute_command not registered")
	}
	if found["check_system_health"] {
		t.Error("check_system_health should NOT be registered when health is nil")
	}
}

func TestRegister_WithHealth(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	reg := registry.New()
	// Use mock health check manager
	mockHealth := &mockHealthCheckManager{report: &ports.HealthReport{
		OverallStatus: ports.StatusHealthy,
		Components:    map[ports.Component]ports.ComponentReport{},
		Timestamp:     time.Now(),
	}}
	err := registerSystem(reg, sm, security.NewCommandValidator(sm, nil), mockHealth)
	if err != nil {
		t.Fatalf("registerSystem failed: %v", err)
	}
	decls := reg.GetDeclarations()
	found := false
	for _, d := range decls {
		if d.Name == "check_system_health" {
			found = true
			break
		}
	}
	if !found {
		t.Error("check_system_health should be registered when health is non-nil")
	}
}
