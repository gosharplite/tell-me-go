// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	sm := &testutil.MockSecurityManager{AllowAll: true}
	exec := &testutil.MockExecutor{}
	validator := &testutil.MockCommandValidator{}
	fs := testutil.NewOSFileSystem()

	if err := Register(registry, sm, exec, validator, fs, nil); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	toolNames := []string{
		"list_files",
		"read_file",
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
		if err := Register(reg, sm, exec, validator, fs, mockHealth); err != nil {
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
