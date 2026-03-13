// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/mock"
)

type mockSessionProvider struct {
	mock.Mock
}

func (m *mockSessionProvider) GetInfo() ports.SessionInfo { return m.Called().Get(0).(ports.SessionInfo) }
func (m *mockSessionProvider) SetInfo(info ports.SessionInfo) { m.Called(info) }
func (m *mockSessionProvider) Close() error { return m.Called().Error(0) }
func (m *mockSessionProvider) GetTasks() ports.ITaskService { return nil }
func (m *mockSessionProvider) GetConfig() ports.IConfigService { return nil }
func (m *mockSessionProvider) GetScratchpad() ports.IScratchpadService { return nil }

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
	_, err := r.Execute(ctx, "list_files", map[string]interface{}{"path": "."})
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

	res, err := r.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{"graph": graph})
	if err != nil {
		t.Fatalf("failed to execute generate_mermaid_diagram: %v", err)
	}

	if res.Text == "" {
		t.Error("expected mermaid output, got empty string")
	}

	// Test invalid graph
	res, err = r.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{"graph": 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error") {
		t.Errorf("expected error message in result, got %s", res.Text)
	}

	// Test missing graph
	res, err = r.Execute(ctx, "generate_mermaid_diagram", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error") {
		t.Errorf("expected error message in result, got %s", res.Text)
	}
}
