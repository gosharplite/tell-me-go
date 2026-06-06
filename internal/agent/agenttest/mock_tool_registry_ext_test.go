// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ---------------------------------------------------------------------------
// RegisterWithOptions — verifies delegation to RegisterToToolkitWithOptions("core", ...)
// ---------------------------------------------------------------------------

func TestMockToolRegistry_RegisterWithOptions_DelegatesToCore(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	def := &tools.ToolDeclaration{Name: "serial_tool"}
	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}

	err := m.RegisterWithOptions(def, handler, tools.ToolOptions{Serial: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Core delegation: tool must appear in ToolkitMap["core"].
	coreDecls := m.ToolkitMap["core"]
	if len(coreDecls) != 1 {
		t.Fatalf("got %d core declarations; want 1", len(coreDecls))
	}
	if coreDecls[0].Name != "serial_tool" {
		t.Errorf("got name %q; want %q", coreDecls[0].Name, "serial_tool")
	}

	// Options: Serial must be true.
	if !m.IsSerial("serial_tool") {
		t.Error("expected IsSerial=true")
	}
}

// ---------------------------------------------------------------------------
// GetOptions — returns m.Options[name]
// ---------------------------------------------------------------------------

func TestMockToolRegistry_GetOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		register   bool
		opts       tools.ToolOptions
		lookupName string
		want       tools.ToolOptions
	}{
		{
			name:       "full options",
			register:   true,
			opts:       tools.ToolOptions{Serial: true, LongRunning: true},
			lookupName: "full",
			want:       tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			name:       "unregistered tool returns zero value",
			register:   false,
			opts:       tools.ToolOptions{},
			lookupName: "nonexistent",
			want:       tools.ToolOptions{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewMockToolRegistry()
			if tt.register {
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterWithOptions(&tools.ToolDeclaration{Name: tt.lookupName}, handler, tt.opts)
			}

			got := m.GetOptions(tt.lookupName)
			if got != tt.want {
				t.Errorf("GetOptions(%q) = %+v; want %+v", tt.lookupName, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetCoreDeclarations — returns m.ToolkitMap["core"]
// ---------------------------------------------------------------------------

func TestMockToolRegistry_GetCoreDeclarations(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}

	_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "core_a"}, handler)
	_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "core_b"}, handler)
	_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "extra_a"}, handler)

	coreDecls := m.GetCoreDeclarations()
	if len(coreDecls) != 2 {
		t.Fatalf("got %d core declarations; want 2", len(coreDecls))
	}

	names := make(map[string]bool)
	for _, d := range coreDecls {
		names[d.Name] = true
	}
	if !names["core_a"] {
		t.Error("expected core_a in core declarations")
	}
	if !names["core_b"] {
		t.Error("expected core_b in core declarations")
	}
	if names["extra_a"] {
		t.Error("extra_a must not appear in core declarations")
	}
}

// ---------------------------------------------------------------------------
// GetDeclarationsByToolkits — dedup, fallback, and core-always-merged
// ---------------------------------------------------------------------------

func TestMockToolRegistry_GetDeclarationsByToolkits_Extended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(m *MockToolRegistry)
		toolkits []string
		want     []string // expected declaration names (order-independent)
	}{
		{
			name: "dedup across toolkits",
			setup: func(m *MockToolRegistry) {
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "shared"}, handler)
				_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "shared"}, handler)
			},
			toolkits: []string{"core", "extra"},
			want:     []string{"shared"},
		},
		{
			name: "nonexistent toolkit returns core declarations",
			setup: func(m *MockToolRegistry) {
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "base"}, handler)
			},
			toolkits: []string{"nonexistent"},
			want:     []string{"base"},
		},
		{
			name: "core tool included even when querying only extra",
			setup: func(m *MockToolRegistry) {
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "core_only"}, handler)
			},
			toolkits: []string{"extra"},
			want:     []string{"core_only"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewMockToolRegistry()
			tt.setup(m)

			result := m.GetDeclarationsByToolkits(tt.toolkits)

			gotNames := make(map[string]bool)
			for _, d := range result {
				gotNames[d.Name] = true
			}

			if len(result) != len(tt.want) {
				t.Errorf("got %d declarations; want %d", len(result), len(tt.want))
			}
			for _, wantName := range tt.want {
				if !gotNames[wantName] {
					t.Errorf("expected declaration %q in result", wantName)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListAvailableToolkits — returns toolkit names
// ---------------------------------------------------------------------------

func TestMockToolRegistry_ListAvailableToolkits_Extended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(m *MockToolRegistry)
		want  []string // expected toolkit names
	}{
		{
			name: "multiple toolkits",
			setup: func(m *MockToolRegistry) {
				handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{}, nil
				}
				_ = m.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "a"}, handler)
				_ = m.RegisterToToolkit("extra", &tools.ToolDeclaration{Name: "b"}, handler)
				_ = m.RegisterToToolkit("custom", &tools.ToolDeclaration{Name: "c"}, handler)
			},
			want: []string{"core", "extra", "custom"},
		},
		{
			name: "empty registry returns empty slice",
			setup: func(m *MockToolRegistry) {
				// no registrations
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := NewMockToolRegistry()
			tt.setup(m)

			got := m.ListAvailableToolkits()

			if len(got) != len(tt.want) {
				t.Fatalf("got %d toolkits; want %d", len(got), len(tt.want))
			}

			gotSet := make(map[string]bool, len(got))
			for _, tk := range got {
				gotSet[tk] = true
			}
			for _, wantTK := range tt.want {
				if !gotSet[wantTK] {
					t.Errorf("expected toolkit %q in list", wantTK)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RegisterToToolkitWithOptions — error path: FailAfter=1 + RegisterErr non-nil
// ---------------------------------------------------------------------------

func TestMockToolRegistry_RegisterToToolkitWithOptions_FailAfterWithErr(t *testing.T) {
	t.Parallel()

	m := NewMockToolRegistry()
	m.SetRegisterErr(errors.New("injected failure"))
	m.SetFailAfter(1)

	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}

	// First call: succeeds (CallCount 1, not > FailAfter=1).
	err := m.RegisterToToolkitWithOptions("core", &tools.ToolDeclaration{Name: "first"}, handler, tools.ToolOptions{})
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if m.CallCount != 1 {
		t.Errorf("after first call: CallCount = %d; want 1", m.CallCount)
	}

	// Second call: fails (CallCount 2 > FailAfter=1).
	err = m.RegisterToToolkitWithOptions("core", &tools.ToolDeclaration{Name: "second"}, handler, tools.ToolOptions{Serial: true})
	if err == nil {
		t.Fatal("second call: expected error, got nil")
	}
	if m.CallCount != 2 {
		t.Errorf("after second call: CallCount = %d; want 2", m.CallCount)
	}

	// Only the first tool should be registered.
	decls := m.GetDeclarations()
	if len(decls) != 1 {
		t.Fatalf("got %d declarations; want 1", len(decls))
	}
	if decls[0].Name != "first" {
		t.Errorf("got name %q; want %q", decls[0].Name, "first")
	}
}

// ---------------------------------------------------------------------------
// RegisterToToolkitWithOptions — zero-value registry (nil maps are lazily initialised)
// ---------------------------------------------------------------------------

func TestMockToolRegistry_RegisterToToolkitWithOptions_ZeroValueRegistry(t *testing.T) {
	t.Parallel()

	// Use a zero-value *MockToolRegistry (not NewMockToolRegistry) so all
	// internal maps start nil. This covers the lazy-init branches.
	m := &MockToolRegistry{}

	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{}, nil
	}

	err := m.RegisterToToolkitWithOptions("core", &tools.ToolDeclaration{Name: "lazy"}, handler, tools.ToolOptions{Serial: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the tool was registered despite nil starting maps.
	if m.ToolkitMap["core"][0].Name != "lazy" {
		t.Errorf("got name %q; want %q", m.ToolkitMap["core"][0].Name, "lazy")
	}
	if !m.IsSerial("lazy") {
		t.Error("expected IsSerial=true")
	}
}
