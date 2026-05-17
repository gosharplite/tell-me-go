// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestMockToolRegistry_Register_AddsToDeclarations(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	decl := &tools.ToolDeclaration{Name: "test_tool"}
	err := m.Register(decl, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decls := m.GetDeclarations()
	if len(decls) != 1 {
		t.Fatalf("got %d declarations; want 1", len(decls))
	}
	if decls[0].Name != "test_tool" {
		t.Errorf("got name %q; want %q", decls[0].Name, "test_tool")
	}
}

func TestMockToolRegistry_RegisterToToolkit_AddsToToolkitMap(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	decl := &tools.ToolDeclaration{Name: "tk_tool"}
	err := m.RegisterToToolkit("mytk", decl, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tkDecls, ok := m.ToolkitMap["mytk"]
	if !ok {
		t.Fatal("ToolkitMap missing key 'mytk'")
	}
	if len(tkDecls) != 1 || tkDecls[0].Name != "tk_tool" {
		t.Errorf("got %v; want [tk_tool]", tkDecls)
	}
}

func TestMockToolRegistry_Execute_FindsHandler(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	decl := &tools.ToolDeclaration{Name: "echo"}
	_ = m.Register(decl, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "ok"}, nil
	})
	result, err := m.Execute(context.Background(), "echo", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "ok" {
		t.Errorf("got Text %q; want %q", result.Text, "ok")
	}
}

func TestMockToolRegistry_Execute_MissingHandler(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	result, err := m.Execute(context.Background(), "nonexistent", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Text != "" {
		t.Errorf("got Text %q; want empty", result.Text)
	}
}

func TestMockToolRegistry_FailAfter_BlocksAfterCount(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	m.SetRegisterErr(errors.New("register blocked"))
	m.SetFailAfter(2)

	decl := &tools.ToolDeclaration{Name: "tool_a"}
	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}
	// First two succeed.
	if err := m.Register(decl, handler); err != nil {
		t.Fatalf("first register: unexpected error: %v", err)
	}
	decl2 := &tools.ToolDeclaration{Name: "tool_b"}
	if err := m.Register(decl2, handler); err != nil {
		t.Fatalf("second register: unexpected error: %v", err)
	}
	// Third fails.
	decl3 := &tools.ToolDeclaration{Name: "tool_c"}
	if err := m.Register(decl3, handler); err == nil {
		t.Fatal("expected error on third register, got nil")
	}
	if len(m.GetDeclarations()) != 2 {
		t.Errorf("got %d declarations; want 2", len(m.GetDeclarations()))
	}
}

func TestMockToolRegistry_RegisterErr_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	m.SetRegisterErr(errors.New("always fail"))
	// FailAfter defaults to 0 — RegisterErr returns immediately.

	decl := &tools.ToolDeclaration{Name: "blocked"}
	err := m.Register(decl, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(m.GetDeclarations()) != 0 {
		t.Errorf("got %d declarations; want 0", len(m.GetDeclarations()))
	}
}

func TestMockToolRegistry_GetDeclarationsByToolkits_Dedup(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}
	_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "shared"}, handler)
	_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "shared"}, handler)

	result := m.GetDeclarationsByToolkits([]string{"core", "extra"})
	if len(result) != 1 {
		t.Errorf("got %d declarations; want 1 (deduplicated)", len(result))
	}
	if result[0].Name != "shared" {
		t.Errorf("got name %q; want %q", result[0].Name, "shared")
	}
}

func TestMockToolRegistry_IsSerial_ReturnsOptions(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	_ = m.RegisterToToolkitWithOptions(
		"core",
		&tools.ToolDeclaration{Name: "slow"},
		func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, nil
		},
		tools.ToolOptions{Serial: true},
	)

	if !m.IsSerial("slow") {
		t.Error("expected IsSerial=true")
	}
}

func TestMockToolRegistry_IsLongRunning_ReturnsOptions(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	_ = m.RegisterToToolkitWithOptions(
		"core",
		&tools.ToolDeclaration{Name: "heavy"},
		func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, nil
		},
		tools.ToolOptions{LongRunning: true},
	)

	if !m.IsLongRunning("heavy") {
		t.Error("expected IsLongRunning=true")
	}
}

func TestMockToolRegistry_ListAvailableToolkits(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}
	_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "a"}, handler)
	_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "b"}, handler)

	toolkits := m.ListAvailableToolkits()
	foundCore := false
	foundExtra := false
	for _, tk := range toolkits {
		if tk == "core" {
			foundCore = true
		}
		if tk == "extra" {
			foundExtra = true
		}
	}
	if !foundCore {
		t.Error("expected 'core' in toolkit list")
	}
	if !foundExtra {
		t.Error("expected 'extra' in toolkit list")
	}
}
