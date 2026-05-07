// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRegistry records RegisterToToolkit calls and implements tools.Registry.
type mockRegistry struct {
	registered []registeredTool
	returnErr  error
}

type registeredTool struct {
	toolkit string
	name    string
	handler tools.ToolFunc
}

// --- tools.ToolRegistrar ---

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.returnErr
}

func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.returnErr
}

func (m *mockRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if m.returnErr != nil {
		return m.returnErr
	}
	m.registered = append(m.registered, registeredTool{toolkit, def.Name, handler})
	return nil
}

func (m *mockRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.returnErr
}

// --- tools.ToolExecutor ---

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockRegistry) IsSerial(name string) bool                { return false }
func (m *mockRegistry) IsLongRunning(name string) bool           { return false }
func (m *mockRegistry) GetOptions(name string) tools.ToolOptions { return tools.ToolOptions{} }

// --- tools.ToolMetadataProvider ---

func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration     { return nil }
func (m *mockRegistry) GetCoreDeclarations() []*tools.ToolDeclaration { return nil }
func (m *mockRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}
func (m *mockRegistry) ListAvailableToolkits() []string { return nil }

// --- Helpers ---

func (m *mockRegistry) names() []string {
	n := make([]string, len(m.registered))
	for i, r := range m.registered {
		n[i] = r.name
	}
	return n
}

func (m *mockRegistry) allToolkit(toolkit string) bool {
	for _, r := range m.registered {
		if r.toolkit != toolkit {
			return false
		}
	}
	return true
}

func (m *mockRegistry) allHandlersNonNil() bool {
	for _, r := range m.registered {
		if r.handler == nil {
			return false
		}
	}
	return true
}

// ============================================================================
// registerBuilds
// ============================================================================

func TestRegisterBuilds(t *testing.T) {
	r := &mockRegistry{}
	m := &AdoManager{}
	f := newPipelineFormatter()

	err := registerBuilds(r, m, f)

	assert.NoError(t, err)
	assert.Len(t, r.registered, 3)
	assert.True(t, r.allToolkit("ado"), "all tools must registered under ado toolkit")
	assert.True(t, r.allHandlersNonNil(), "all handlers must be non-nil")
	assert.Contains(t, r.names(), "ado_get_build_timeline")
	assert.Contains(t, r.names(), "ado_get_task_log")
	assert.Contains(t, r.names(), "ado_get_build_changes")
}

func TestRegisterBuilds_ErrorPropagation(t *testing.T) {
	r := &mockRegistry{returnErr: fmt.Errorf("mock error")}
	m := &AdoManager{}
	f := newPipelineFormatter()

	err := registerBuilds(r, m, f)
	assert.Error(t, err)
	assert.Equal(t, "mock error", err.Error())
}

// ============================================================================
// registerPipelines
// ============================================================================

func TestRegisterPipelines(t *testing.T) {
	r := &mockRegistry{}
	m := &AdoManager{}
	f := newPipelineFormatter()

	err := registerPipelines(r, m, f)

	assert.NoError(t, err)
	assert.Len(t, r.registered, 7)
	assert.True(t, r.allToolkit("ado"))
	assert.True(t, r.allHandlersNonNil())

	wantNames := []string{
		"ado_list_pipelines",
		"ado_list_pipeline_runs",
		"ado_get_pipeline_run",
		"ado_get_pipeline_definition",
		"ado_get_pipeline_logs",
		"ado_create_pipeline",
		"ado_run_pipeline",
	}
	for _, name := range wantNames {
		assert.Contains(t, r.names(), name)
	}
}

func TestRegisterPipelines_ErrorPropagation(t *testing.T) {
	r := &mockRegistry{returnErr: fmt.Errorf("mock error")}
	m := &AdoManager{}
	f := newPipelineFormatter()

	err := registerPipelines(r, m, f)
	assert.Error(t, err)
}

// ============================================================================
// registerPolicy
// ============================================================================

func TestRegisterPolicy(t *testing.T) {
	r := &mockRegistry{}
	m := &AdoManager{}
	f := newPipelineFormatter()

	err := registerPolicy(r, m, f)

	assert.NoError(t, err)
	assert.Len(t, r.registered, 2)
	assert.True(t, r.allToolkit("ado"))
	assert.True(t, r.allHandlersNonNil())

	wantNames := []string{
		"ado_list_branch_policies",
		"ado_update_build_definition_variables",
	}
	for _, name := range wantNames {
		assert.Contains(t, r.names(), name)
	}
}

func TestRegisterPolicy_ErrorPropagation(t *testing.T) {
	r := &mockRegistry{returnErr: fmt.Errorf("mock error")}
	m := &AdoManager{}
	f := newPipelineFormatter()

	err := registerPolicy(r, m, f)
	assert.Error(t, err)
}

// ============================================================================
// registerPullRequests
// ============================================================================

func TestRegisterPullRequests(t *testing.T) {
	r := &mockRegistry{}
	m := &AdoManager{}

	err := registerPullRequests(r, m, nil)

	assert.NoError(t, err)
	assert.Len(t, r.registered, 6)
	assert.True(t, r.allToolkit("ado"))
	assert.True(t, r.allHandlersNonNil())

	wantNames := []string{
		"ado_get_pull_request",
		"ado_list_pull_requests",
		"ado_get_pr_diff",
		"ado_get_pr_threads",
		"ado_get_pr_statuses",
		"ado_get_pr_policy_evaluations",
	}
	for _, name := range wantNames {
		assert.Contains(t, r.names(), name)
	}
}

// ============================================================================
// registerRepository
// ============================================================================

func TestRegisterRepository(t *testing.T) {
	r := &mockRegistry{}
	m := &AdoManager{}

	err := registerRepository(r, m, nil)

	assert.NoError(t, err)
	assert.Len(t, r.registered, 2)
	assert.True(t, r.allToolkit("ado"))
	assert.True(t, r.allHandlersNonNil())

	wantNames := []string{
		"ado_get_file_content",
		"ado_list_repository_items",
	}
	for _, name := range wantNames {
		assert.Contains(t, r.names(), name)
	}
}

// ============================================================================
// Register (top-level)
// ============================================================================

func TestRegister(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		r := &mockRegistry{}
		sm := &toolstest.MockSecurityManager{AllowAll: true}

		// No AZURE_PAT_ALL env — Register should still succeed (token is optional
		// at registration time; the handlers check it at execution time).
		err := Register(r, sm, nil)

		require.NoError(t, err)
		assert.NotEmpty(t, r.registered)
		assert.True(t, r.allToolkit("ado"), "all tools must be registered under ado toolkit")
		assert.True(t, r.allHandlersNonNil(), "all handlers must be non-nil")

		// Verify all 20 tools are registered (6 PR + 7 Pipeline + 3 Build + 2 Repo + 2 Policy)
		assert.Len(t, r.registered, 20)
	})

	t.Run("Error propagation stops registration", func(t *testing.T) {
		// Use a mockRegistry where RegisterToToolkit succeeds once and fails
		// on the second call.
		r := &failingRegistry{maxCalls: 1}

		sm := &toolstest.MockSecurityManager{AllowAll: true}
		err := Register(r, sm, nil)

		assert.Error(t, err)
		assert.Len(t, r.registered, 1)
	})
}

// failingRegistry succeeds for the first maxCalls then errors.
type failingRegistry struct {
	mockRegistry
	maxCalls int
	callNum  int
}

func (f *failingRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if f.callNum >= f.maxCalls {
		return fmt.Errorf("registration limit exceeded")
	}
	f.callNum++
	f.registered = append(f.registered, registeredTool{toolkit, def.Name, handler})
	return nil
}
