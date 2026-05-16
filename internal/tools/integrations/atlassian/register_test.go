// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockRegistry — test double for tools.Registry
// ---------------------------------------------------------------------------

type mockRegistry struct {
	toolkitRegs []toolkitRegistration

	// Error providers: called with the 1-based call number; return nil to succeed.
	registerToToolkitErrProvider            func(callNum int) error
	registerToToolkitWithOptionsErrProvider func(callNum int) error

	registerToToolkitCallCount            int
	registerToToolkitWithOptionsCallCount int
}

type toolkitRegistration struct {
	toolkit string
	def     *tools.ToolDeclaration
	handler tools.ToolFunc
	opts    tools.ToolOptions
	hasOpts bool // true if registered via RegisterToToolkitWithOptions
}

// --- ToolRegistrar ---

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return errors.New("Register: not expected in atlassian registration tests")
}

func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return errors.New("RegisterWithOptions: not expected in atlassian registration tests")
}

func (m *mockRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	m.registerToToolkitCallCount++
	if m.registerToToolkitErrProvider != nil {
		if err := m.registerToToolkitErrProvider(m.registerToToolkitCallCount); err != nil {
			return err
		}
	}
	m.toolkitRegs = append(m.toolkitRegs, toolkitRegistration{
		toolkit: toolkit, def: def, handler: handler,
	})
	return nil
}

func (m *mockRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.registerToToolkitWithOptionsCallCount++
	if m.registerToToolkitWithOptionsErrProvider != nil {
		if err := m.registerToToolkitWithOptionsErrProvider(m.registerToToolkitWithOptionsCallCount); err != nil {
			return err
		}
	}
	m.toolkitRegs = append(m.toolkitRegs, toolkitRegistration{
		toolkit: toolkit, def: def, handler: handler,
		opts: opts, hasOpts: true,
	})
	return nil
}

// --- ToolExecutor (stubs) ---

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockRegistry) IsSerial(name string) bool                { return false }
func (m *mockRegistry) IsLongRunning(name string) bool           { return false }
func (m *mockRegistry) GetOptions(name string) tools.ToolOptions { return tools.ToolOptions{} }

// --- ToolMetadataProvider (stubs) ---

func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration     { return nil }
func (m *mockRegistry) GetCoreDeclarations() []*tools.ToolDeclaration { return nil }
func (m *mockRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}
func (m *mockRegistry) ListAvailableToolkits() []string { return nil }

// Compile-time check
var _ tools.Registry = (*mockRegistry)(nil)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func setAtlassianEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ATLASSIAN_BASE_URL", "https://test.atlassian.net")
	t.Setenv("ATLASSIAN_EMAIL", "test@example.com")
	t.Setenv("ATLASSIAN_TOKEN", "api-token")
}

func findReg(regs []toolkitRegistration, name string) (toolkitRegistration, bool) {
	for _, r := range regs {
		if r.def.Name == name {
			return r, true
		}
	}
	return toolkitRegistration{}, false
}

// sm is the security manager used by RegisterConfluence/RegisterJira.
var sm security.Manager = &toolstest.MockSecurityManager{AllowAll: true}

// ---------------------------------------------------------------------------
// RegisterConfluence
// ---------------------------------------------------------------------------

func TestRegisterConfluence(t *testing.T) {
	t.Run("Success_AllThreeTools", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}

		err := RegisterConfluence(reg, sm, nil)
		require.NoError(t, err)
		require.Len(t, reg.toolkitRegs, 3)

		// --- confluence_search ---
		search, ok := findReg(reg.toolkitRegs, "confluence_search")
		require.True(t, ok, "confluence_search should be registered")
		assert.Equal(t, "confluence", search.toolkit)
		assert.NotNil(t, search.handler)
		assert.False(t, search.hasOpts)

		// --- confluence_read ---
		read, ok := findReg(reg.toolkitRegs, "confluence_read")
		require.True(t, ok, "confluence_read should be registered")
		assert.Equal(t, "confluence", read.toolkit)
		assert.NotNil(t, read.handler)
		assert.False(t, read.hasOpts)
		require.NotNil(t, read.def.Parameters)
		assert.Contains(t, read.def.Parameters.Required, "page_id")

		// --- confluence_write ---
		write, ok := findReg(reg.toolkitRegs, "confluence_write")
		require.True(t, ok, "confluence_write should be registered")
		assert.Equal(t, "confluence", write.toolkit)
		assert.NotNil(t, write.handler)
		assert.True(t, write.hasOpts, "confluence_write should use RegisterToToolkitWithOptions")
		assert.True(t, write.def.RequiresConsent, "confluence_write should require consent")
		assert.True(t, write.opts.Serial, "confluence_write should have Serial: true")
		require.NotNil(t, write.def.Parameters)
		assert.Contains(t, write.def.Parameters.Required, "page_id")
		assert.Contains(t, write.def.Parameters.Required, "markdown_content")
	})

	t.Run("ProviderInitFailure_Skips", func(t *testing.T) {
		// Explicitly clear ATLASSIAN_BASE_URL so NewAtlassianProvider fails.
		t.Setenv("ATLASSIAN_BASE_URL", "")
		reg := &mockRegistry{}

		err := RegisterConfluence(reg, sm, nil)
		assert.NoError(t, err)
		assert.Empty(t, reg.toolkitRegs)
	})

	t.Run("RegisterToToolkit_Error_Propagates_FirstCall", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		testErr := errors.New("register toolkit boom")

		// Fail on the 1st RegisterToToolkit call (confluence_search).
		reg.registerToToolkitErrProvider = func(callNum int) error {
			if callNum == 1 {
				return testErr
			}
			return nil
		}

		err := RegisterConfluence(reg, sm, nil)
		assert.ErrorIs(t, err, testErr)
		assert.Empty(t, reg.toolkitRegs)
	})

	t.Run("RegisterToToolkit_Error_Propagates_SecondCall", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		testErr := errors.New("register toolkit boom")

		// Fail on the 2nd RegisterToToolkit call (confluence_read).
		reg.registerToToolkitErrProvider = func(callNum int) error {
			if callNum == 2 {
				return testErr
			}
			return nil
		}

		err := RegisterConfluence(reg, sm, nil)
		assert.ErrorIs(t, err, testErr)
		// Only the first tool (search) should have been recorded before the error.
		assert.Len(t, reg.toolkitRegs, 1)
	})

	t.Run("RegisterToToolkitWithOptions_Error_Propagates", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		testErr := errors.New("register with options boom")

		reg.registerToToolkitWithOptionsErrProvider = func(_ int) error {
			return testErr
		}

		err := RegisterConfluence(reg, sm, nil)
		assert.ErrorIs(t, err, testErr)
		// The first two calls (RegisterToToolkit) should have succeeded.
		assert.Len(t, reg.toolkitRegs, 2)
	})
}

// ---------------------------------------------------------------------------
// RegisterJira
// ---------------------------------------------------------------------------

func TestRegisterJira(t *testing.T) {
	t.Run("Success_BothTools", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}

		err := RegisterJira(reg, sm, nil)
		require.NoError(t, err)
		require.Len(t, reg.toolkitRegs, 2)

		// --- jira_search_issues ---
		search, ok := findReg(reg.toolkitRegs, "jira_search_issues")
		require.True(t, ok, "jira_search_issues should be registered")
		assert.Equal(t, "jira", search.toolkit)
		assert.NotNil(t, search.handler)
		assert.False(t, search.hasOpts)
		require.NotNil(t, search.def.Parameters)
		assert.Contains(t, search.def.Parameters.Required, "jql")

		// --- jira_get_issue ---
		get, ok := findReg(reg.toolkitRegs, "jira_get_issue")
		require.True(t, ok, "jira_get_issue should be registered")
		assert.Equal(t, "jira", get.toolkit)
		assert.NotNil(t, get.handler)
		assert.False(t, get.hasOpts)
		require.NotNil(t, get.def.Parameters)
		assert.Contains(t, get.def.Parameters.Required, "issue_key")
	})

	t.Run("ProviderInitFailure_Skips", func(t *testing.T) {
		t.Setenv("ATLASSIAN_BASE_URL", "")
		reg := &mockRegistry{}

		err := RegisterJira(reg, sm, nil)
		assert.NoError(t, err)
		assert.Empty(t, reg.toolkitRegs)
	})

	t.Run("RegisterToToolkit_Error_Propagates_FirstCall", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		testErr := errors.New("register toolkit boom")

		// Fail on the 1st RegisterToToolkit call (jira_search_issues).
		reg.registerToToolkitErrProvider = func(callNum int) error {
			if callNum == 1 {
				return testErr
			}
			return nil
		}

		err := RegisterJira(reg, sm, nil)
		assert.ErrorIs(t, err, testErr)
		assert.Empty(t, reg.toolkitRegs)
	})

	t.Run("RegisterToToolkit_Error_Propagates_SecondCall", func(t *testing.T) {
		setAtlassianEnv(t)
		reg := &mockRegistry{}
		testErr := errors.New("register toolkit boom")

		// Fail on the 2nd RegisterToToolkit call (jira_get_issue).
		reg.registerToToolkitErrProvider = func(callNum int) error {
			if callNum == 2 {
				return testErr
			}
			return nil
		}

		err := RegisterJira(reg, sm, nil)
		assert.ErrorIs(t, err, testErr)
		// Only the first tool (search) should have been recorded before the error.
		assert.Len(t, reg.toolkitRegs, 1)
	})
}
