// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
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
// failingRegistry — mock that injects failures at a specific call index
// ---------------------------------------------------------------------------

type failingRegistry struct {
	*mockToolRegistry
	failAt    int
	callCount int
}

func (f *failingRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	f.callCount++
	if f.callCount == f.failAt {
		return fmt.Errorf("injected failure at call %d", f.callCount)
	}
	return f.mockToolRegistry.Register(def, handler)
}

func (f *failingRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	f.callCount++
	if f.callCount == f.failAt {
		return fmt.Errorf("injected failure at call %d", f.callCount)
	}
	return f.mockToolRegistry.RegisterWithOptions(def, handler, opts)
}

func (f *failingRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	f.callCount++
	if f.callCount == f.failAt {
		return fmt.Errorf("injected failure at call %d", f.callCount)
	}
	return f.mockToolRegistry.RegisterToToolkit(toolkit, def, handler)
}

func (f *failingRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	f.callCount++
	if f.callCount == f.failAt {
		return fmt.Errorf("injected failure at call %d", f.callCount)
	}
	return f.mockToolRegistry.RegisterToToolkitWithOptions(toolkit, def, handler, opts)
}

// ---------------------------------------------------------------------------
// Partial-failure tests
// ---------------------------------------------------------------------------

func TestRegisterFiles_PartialFailure(t *testing.T) {
	registry := &failingRegistry{
		mockToolRegistry: &mockToolRegistry{},
		failAt:           5, // fail on the 5th tool (search_files)
	}
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	exec := &toolstest.MockExecutor{}
	fs := persistencetest.NewPlainOSFileSystem()
	wp := infra_persistence.NewWorkspacePolicy()

	err := registerFiles(registry, sm, fs, exec, wp)
	if err == nil {
		t.Fatal("expected error from registerFiles partial failure")
	}
	if !strings.Contains(err.Error(), "injected failure") {
		t.Errorf("expected 'injected failure' in error, got %q", err.Error())
	}

	// Verify that tools before the failure were registered (callCount should be 5)
	if registry.callCount != 5 {
		t.Errorf("expected 5 registration calls before failure, got %d", registry.callCount)
	}
}

func TestRegisterSystem_PartialFailure(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}

	t.Run("execute_command fails", func(t *testing.T) {
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failAt:           1,
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
			failAt:           2,
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
			failAt:           3,
		}
		err := registerSystem(registry, sm, validator, nil)
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
			failAt:           1, // get_git_status
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
			failAt:           4, // get_git_blame
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
			failAt:           6, // git_commit (first RegisterToToolkitWithOptions)
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
		// 13 file tools succeed, then system fails on call 14 (execute_command)
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failAt:           14,
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
		// 13 file tools + 3 system tools = 16, then git fails on call 17 (get_git_status)
		registry := &failingRegistry{
			mockToolRegistry: &mockToolRegistry{},
			failAt:           17,
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
