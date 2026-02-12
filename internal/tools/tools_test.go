// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools"
)

func TestNewToolRegistry(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	tools.RegisterAll(r, sm, t.TempDir(), "tokens.log", "model", "mode", nil, nil, t.TempDir(), nil)

	if len(r.GetDeclarations()) == 0 {
		t.Error("expected registered tools, got none")
	}
}

func TestToolExecution(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	tools.RegisterAll(r, sm, t.TempDir(), "tokens.log", "model", "mode", nil, nil, t.TempDir(), nil)

	ctx := context.Background()
	// list_files is registered by files.Register
	_, err := r.Execute(ctx, "list_files", map[string]interface{}{"path": "."})
	if err != nil {
		t.Errorf("failed to execute list_files: %v", err)
	}
}

func TestGenerateMermaidDiagram(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	tools.RegisterAll(r, sm, t.TempDir(), "tokens.log", "model", "mode", nil, nil, t.TempDir(), nil)

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
