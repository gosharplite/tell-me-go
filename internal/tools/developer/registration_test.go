// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
)

type mockToolRegistry struct {
	tools map[string]bool
}

func (m *mockToolRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if m.tools == nil {
		m.tools = make(map[string]bool)
	}
	m.tools[def.Name] = true
	return nil
}

func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.Register(def, handler)
}

func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockToolRegistry) IsSerial(name string) bool      { return false }
func (m *mockToolRegistry) IsLongRunning(name string) bool { return false }
func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return nil
}

func TestRegister(t *testing.T) {
	t.Parallel()
	registry := &mockToolRegistry{}
	sm := security.NewSecurityManager(nil)
	validator := security.NewCommandValidator(sm, nil)
	fs := persistence.NewMockFileSystem()
	exec := &mockCommandExecutor{}

	if err := Register(registry, sm, exec, validator, fs); err != nil {
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
