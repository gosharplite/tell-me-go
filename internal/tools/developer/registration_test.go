// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

type mockToolRegistry struct {
	tools  map[string]bool
	failOn string // tool name that should trigger Register error
}

func (m *mockToolRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if m.tools == nil {
		m.tools = make(map[string]bool)
	}
	m.tools[def.Name] = true
	return nil
}

func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if m.failOn != "" && def.Name == m.failOn {
		return fmt.Errorf("simulated registration failure for %s", def.Name)
	}
	return m.Register(def, handler)
}

func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockToolRegistry) IsSerial(name string) bool      { return false }
func (m *mockToolRegistry) IsLongRunning(name string) bool { return false }
func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return nil
}

type mockToolchainExecutor struct {
	runFunc      func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPathFunc func(file string) (string, error)
}

func (m *mockToolchainExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	return []byte(""), nil
}

func (m *mockToolchainExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.runFunc != nil {
		return m.runFunc(ctx, name, args...)
	}
	return []byte(""), nil
}

func (m *mockToolchainExecutor) LookPath(file string) (string, error) {
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return "/usr/bin/" + file, nil
}

func TestRegister(t *testing.T) {
	t.Parallel()
	registry := &mockToolRegistry{}
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}
	fs := persistence.NewMockFileSystem()
	exec := &mockToolchainExecutor{}

	if err := Register(registry, sm, exec, validator, fs, infra_persistence.NewWorkspacePolicy(), nil); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	toolNames := []string{
		"run_tests",
		"go_tidy",
		"get_coverage",
		"run_linter",
		"run_benchmark",
		"check_vulnerabilities",
		"verify_release_readiness",
	}

	for _, name := range toolNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, registry.tools[name], "tool %s should be registered", name)
		})
	}
}

func TestRegister_PartialFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		failOn            string
		wantRegistered    []string
		wantNotRegistered string
	}{
		{
			name:              "First tool fails",
			failOn:            "run_tests",
			wantRegistered:    nil,
			wantNotRegistered: "run_tests",
		},
		{
			name:              "Middle tool fails",
			failOn:            "run_linter",
			wantRegistered:    []string{"run_tests", "go_tidy", "get_coverage"},
			wantNotRegistered: "run_linter",
		},
		{
			name:              "Last tool fails",
			failOn:            "verify_release_readiness",
			wantRegistered:    []string{"run_tests", "go_tidy", "get_coverage", "run_linter", "run_benchmark", "check_vulnerabilities"},
			wantNotRegistered: "verify_release_readiness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registry := &mockToolRegistry{failOn: tt.failOn}
			sm := &toolstest.MockSecurityManager{AllowAll: true}
			validator := &toolstest.MockCommandValidator{}
			fs := persistence.NewMockFileSystem()
			exec := &mockToolchainExecutor{}

			err := Register(registry, sm, exec, validator, fs, infra_persistence.NewWorkspacePolicy(), nil)

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.failOn) {
				t.Errorf("error %q should contain %q", err.Error(), tt.failOn)
			}

			for _, name := range tt.wantRegistered {
				if !registry.tools[name] {
					t.Errorf("tool %q should be registered before failure", name)
				}
			}
			if registry.tools[tt.wantNotRegistered] {
				t.Errorf("tool %q should NOT be registered (was the failing one)", tt.wantNotRegistered)
			}
		})
	}
}

func TestRegister_DuplicateRegistration(t *testing.T) {
	t.Parallel()
	registry := &mockToolRegistry{}
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}
	fs := persistence.NewMockFileSystem()
	exec := &mockToolchainExecutor{}

	// First registration
	if err := Register(registry, sm, exec, validator, fs, infra_persistence.NewWorkspacePolicy(), nil); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	allTools := []string{
		"run_tests",
		"go_tidy",
		"get_coverage",
		"run_linter",
		"run_benchmark",
		"check_vulnerabilities",
		"verify_release_readiness",
	}

	for _, name := range allTools {
		assert.True(t, registry.tools[name], "tool %s should be registered after first call", name)
	}

	// Second registration
	if err := Register(registry, sm, exec, validator, fs, infra_persistence.NewWorkspacePolicy(), nil); err != nil {
		t.Fatalf("second Register failed: %v", err)
	}

	for _, name := range allTools {
		assert.True(t, registry.tools[name], "tool %s should still be registered after second call", name)
	}
}

func (m *mockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
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
