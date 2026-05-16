// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestMockToolRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockToolRegistry
		check func(t *testing.T, m *MockToolRegistry)
	}{
		{
			name: "register_adds_to_declarations",
			setup: func() *MockToolRegistry {
				return NewMockToolRegistry()
			},
			check: func(t *testing.T, m *MockToolRegistry) {
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
			},
		},
		{
			name: "register_adds_to_toolkit_map",
			setup: func() *MockToolRegistry {
				return NewMockToolRegistry()
			},
			check: func(t *testing.T, m *MockToolRegistry) {
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
			},
		},
		{
			name: "execute_finds_handler",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				decl := &tools.ToolDeclaration{Name: "echo"}
				_ = m.Register(decl, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "ok"}, nil
				})
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
				result, err := m.Execute(context.Background(), "echo", nil, nil)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result.Text != "ok" {
					t.Errorf("got Text %q; want %q", result.Text, "ok")
				}
			},
		},
		{
			name: "execute_missing_handler",
			setup: func() *MockToolRegistry {
				return NewMockToolRegistry()
			},
			check: func(t *testing.T, m *MockToolRegistry) {
				result, err := m.Execute(context.Background(), "nonexistent", nil, nil)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result.Text != "" {
					t.Errorf("got Text %q; want empty", result.Text)
				}
			},
		},
		{
			name: "FailAfter_blocks_after_count",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				m.SetRegisterErr(errors.New("register blocked"))
				m.SetFailAfter(2)
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
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
			},
		},
		{
			name: "RegisterErr_nil_FailAfter_zero",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				m.SetRegisterErr(errors.New("always fail"))
				// FailAfter defaults to 0 — RegisterErr returns immediately.
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
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
			},
		},
		{
			name: "GetDeclarationsByToolkits_dedup",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "shared"}, handler)
				_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "shared"}, handler)
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
				result := m.GetDeclarationsByToolkits([]string{"core", "extra"})
				if len(result) != 1 {
					t.Errorf("got %d declarations; want 1 (deduplicated)", len(result))
				}
				if result[0].Name != "shared" {
					t.Errorf("got name %q; want %q", result[0].Name, "shared")
				}
			},
		},
		{
			name: "IsSerial_returns_options",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				_ = m.RegisterToToolkitWithOptions(
					"core",
					&tools.ToolDeclaration{Name: "slow"},
					func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
						return tools.ToolResult{}, nil
					},
					tools.ToolOptions{Serial: true},
				)
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
				if !m.IsSerial("slow") {
					t.Error("expected IsSerial=true")
				}
			},
		},
		{
			name: "IsLongRunning_returns_options",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				_ = m.RegisterToToolkitWithOptions(
					"core",
					&tools.ToolDeclaration{Name: "heavy"},
					func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
						return tools.ToolResult{}, nil
					},
					tools.ToolOptions{LongRunning: true},
				)
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
				if !m.IsLongRunning("heavy") {
					t.Error("expected IsLongRunning=true")
				}
			},
		},
		{
			name: "ListAvailableToolkits",
			setup: func() *MockToolRegistry {
				m := NewMockToolRegistry()
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "a"}, handler)
				_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "b"}, handler)
				return m
			},
			check: func(t *testing.T, m *MockToolRegistry) {
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
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := tt.setup()
			tt.check(t, m)
		})
	}
}
