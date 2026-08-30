// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
)

func TestNewToolRegistry(t *testing.T) {
	t.Parallel()
	params := newTestParams()
	params.LogFile = "tokens.log"
	params.Model = "model"
	params.Mode = "mode"
	params.AssetsDir = t.TempDir()
	if err := tools.RegisterAll(params); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	if len(params.Registry.GetDeclarations()) == 0 {
		t.Error("expected registered tools, got none")
	}
}

// newTestParams returns a ToolRegistrationParams with every guarded field set
// (8-guard table per issue #1325 / ADR-060) and a real non-failing registry.
// Tests override individual fields to exercise specific paths. Client,
// SessionProvider, and HealthManager stay unset (documented exclusions).
func newTestParams() tools.ToolRegistrationParams {
	return tools.ToolRegistrationParams{
		Registry:         registry.New(),
		SecurityManager:  &toolstest.MockSecurityManager{AllowAll: true},
		WorkspacePolicy:  infra_persistence.NewWorkspacePolicy(),
		FileSystem:       persistencetest.NewPlainOSFileSystem(),
		CommandExecutor:  &toolstest.MockExecutor{},
		CommandValidator: &toolstest.MockCommandValidator{},
		EventBus:         &eventstest.TestEventBus{},
		ToolchainRunner:  &toolstest.FakeToolchainRunner{},
		ProcessRunner:    &toolstest.FakeProcessRunner{},
	}
}

func TestRegisterAll_WithSessionProvider(t *testing.T) {
	t.Parallel()
	sp := &testfixtures.MockSessionProvider{}
	params := newTestParams()
	params.SessionProvider = sp
	params.LogFile = "tokens.log"
	params.Model = "model"
	params.Mode = "mode"
	params.AssetsDir = t.TempDir()
	if err := tools.RegisterAll(params); err != nil {
		t.Fatalf("RegisterAll with SessionProvider failed: %v", err)
	}
}

func TestToolExecution(t *testing.T) {
	t.Parallel()
	params := newTestParams()
	params.LogFile = "tokens.log"
	params.Model = "model"
	params.Mode = "mode"
	params.AssetsDir = t.TempDir()
	if err := tools.RegisterAll(params); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	ctx := context.Background()
	// list_files is registered by workspace.Register
	_, err := params.Registry.Execute(ctx, "list_files", map[string]interface{}{"path": "."}, nil)
	if err != nil {
		t.Errorf("failed to execute list_files: %v", err)
	}
}

func TestGenerateMermaidDiagram(t *testing.T) {
	t.Parallel()
	params := newTestParams()
	params.LogFile = "tokens.log"
	params.Model = "model"
	params.Mode = "mode"
	params.AssetsDir = t.TempDir()
	if err := tools.RegisterAll(params); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	ctx := context.Background()
	graph := map[string]interface{}{
		"pkg1": []interface{}{"pkg2", "pkg3"},
		"pkg2": []string{"pkg3"},
	}

	res, err := params.Registry.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{"graph": graph}, nil)
	if err != nil {
		t.Fatalf("failed to execute generate_mermaid_diagram: %v", err)
	}

	if res.Text == "" {
		t.Error("expected mermaid output, got empty string")
	}

	// Test invalid graph
	res, err = params.Registry.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{"graph": 123}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error") {
		t.Errorf("expected error message in result, got %s", res.Text)
	}

	// Test missing graph
	res, err = params.Registry.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{}, nil)
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

	t.Run("nil Registry returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.Registry = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Registry is required")
	})

	t.Run("nil SecurityManager returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.SecurityManager = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SecurityManager is required")
	})

	t.Run("nil WorkspacePolicy returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.WorkspacePolicy = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "WorkspacePolicy is required")
	})

	t.Run("nil FileSystem returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.FileSystem = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "FileSystem is required")
	})

	t.Run("nil CommandExecutor returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.CommandExecutor = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CommandExecutor is required")
	})

	t.Run("nil CommandValidator returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.CommandValidator = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CommandValidator is required")
	})

	t.Run("nil EventBus returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.EventBus = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "EventBus is required")
	})

	t.Run("nil ToolchainRunner returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.ToolchainRunner = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ToolchainRunner is required")
	})

	t.Run("nil ProcessRunner returns error", func(t *testing.T) {
		t.Parallel()
		params := newTestParams()
		params.ProcessRunner = nil
		err := tools.RegisterAll(params)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ProcessRunner is required")
	})

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
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
		},
		{
			name:      "fail in analysis.Register",
			failAfter: 25,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
		},
		{
			name:      "fail in developer.Register",
			failAfter: 46,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
		},
		{
			name:      "fail in integrations.RegisterAll",
			failAfter: 53,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := newTestParams()
			params.Registry = &MockRegistry{failAfter: tt.failAfter}
			if tt.setup != nil {
				tt.setup(&params)
			}

			err := tools.RegisterAll(params)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "simulated registration failure")
		})
	}
}

func TestRegisterAll_ErrorWrapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		failAfter     int
		setup         func(*tools.ToolRegistrationParams)
		wantSubstring string
	}{
		{
			name:          "workspace.Register wraps error",
			failAfter:     0,
			wantSubstring: "workspace.Register",
		},
		{
			name:      "workspace.RegisterPersistence wraps error",
			failAfter: 23,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
			wantSubstring: "workspace.RegisterPersistence",
		},
		{
			name:      "analysis.Register wraps error",
			failAfter: 26,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
			wantSubstring: "analysis.Register",
		},
		{
			name:      "developer.Register wraps error",
			failAfter: 47,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
			wantSubstring: "developer.Register",
		},
		{
			name:      "integrations.RegisterAll wraps error",
			failAfter: 54,
			setup: func(p *tools.ToolRegistrationParams) {
				p.SessionProvider = &testfixtures.MockSessionProvider{}
			},
			wantSubstring: "integrations.RegisterAll",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := newTestParams()
			params.Registry = &MockRegistry{failAfter: tt.failAfter}
			if tt.setup != nil {
				tt.setup(&params)
			}

			err := tools.RegisterAll(params)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSubstring)
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

// TestRegisterAll_GoDoc_ReachesFakeRunner proves infoManager.Runner is wired
// through RegisterAll (issue #1325, ADR-060): the fake ToolchainRunner's
// GetGoDoc must be reached via registry.Execute("go_doc", ...). Zero
// subprocesses — the fake returns no output and the handler completes.
// hb nil is safe: GoDoc's heartbeat goroutine guards `if hb != nil`.
func TestRegisterAll_GoDoc_ReachesFakeRunner(t *testing.T) {
	t.Parallel()
	fake := &toolstest.FakeToolchainRunner{}
	params := newTestParams()
	params.ToolchainRunner = fake
	if err := tools.RegisterAll(params); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}
	if _, err := params.Registry.Execute(context.Background(), "go_doc", map[string]interface{}{"symbol": "fmt.Println"}, nil); err != nil {
		t.Fatalf("go_doc failed: %v", err)
	}
	if !fake.Called("GetGoDoc") {
		t.Error("fake runner GetGoDoc was not reached through the go_doc handler")
	}
}

// fakeMCPClient is a hand-rolled function-field test double for
// tools.MCPClient (ADR-021). It is local to this test file because the
// domain and mcp test doubles are unexported and tools_test is an external
// test package.
type fakeMCPClient struct {
	listToolsFunc func(ctx context.Context) ([]domaintools.MCPToolDefinition, error)
	callToolFunc  func(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error)
}

func (f *fakeMCPClient) ListTools(ctx context.Context) ([]domaintools.MCPToolDefinition, error) {
	if f.listToolsFunc != nil {
		return f.listToolsFunc(ctx)
	}
	return nil, nil
}

func (f *fakeMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error) {
	if f.callToolFunc != nil {
		return f.callToolFunc(ctx, name, args)
	}
	return domaintools.ToolResult{}, nil
}

func (f *fakeMCPClient) Close() error { return nil }

var _ domaintools.MCPClient = (*fakeMCPClient)(nil)

// TestRegisterAll_MCPTools verifies end-to-end registration of MCP-backed
// tools through tools.RegisterAll: the mcp plugin discovers a tool from the
// injected MCP client, namespaces it, and exposes it for execution through the
// real registry.
func TestRegisterAll_MCPTools(t *testing.T) {
	t.Parallel()

	fake := &fakeMCPClient{
		listToolsFunc: func(ctx context.Context) ([]domaintools.MCPToolDefinition, error) {
			return []domaintools.MCPToolDefinition{
				{Name: "search", Description: "search the server"},
			}, nil
		},
		callToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error) {
			return domaintools.ToolResult{Text: "mcp-result"}, nil
		},
	}

	params := newTestParams()
	params.MCPClients = map[string]plugin.MCPServerDependency{
		"github": {Client: fake, RequiresConsent: false, Serial: false},
	}

	if err := tools.RegisterAll(params); err != nil {
		t.Fatalf("RegisterAll failed: %v", err)
	}

	var found *domaintools.ToolDeclaration
	for _, d := range params.Registry.GetDeclarations() {
		if d.Name == "mcp_github_search" {
			found = d
			break
		}
	}
	if found == nil {
		t.Fatal("mcp_github_search declaration not present in registry")
	}
	if found.RequiresConsent {
		t.Error("mcp_github_search RequiresConsent = true, want false")
	}

	res, err := params.Registry.Execute(context.Background(), "mcp_github_search", map[string]interface{}{"q": "tell-me-go"}, nil)
	if err != nil {
		t.Fatalf("Execute(mcp_github_search) failed: %v", err)
	}
	if res.Text != "mcp-result" {
		t.Errorf("Execute(mcp_github_search) Text = %q, want %q", res.Text, "mcp-result")
	}
}
