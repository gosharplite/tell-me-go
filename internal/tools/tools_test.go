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
