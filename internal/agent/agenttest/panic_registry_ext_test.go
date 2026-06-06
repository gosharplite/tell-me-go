// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestPanicRegistry_GetOptions_SerialTrue(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{Serial: true}
	opts := r.GetOptions("any")
	if !opts.Serial {
		t.Error("got Serial=false; want true")
	}
}

func TestPanicRegistry_GetOptions_SerialFalse(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{Serial: false}
	opts := r.GetOptions("any")
	if opts.Serial {
		t.Error("got Serial=true; want false")
	}
}

func TestPanicRegistry_RegisterToToolkit_NoError(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{}
	err := r.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "t"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanicRegistry_RegisterToToolkitWithOptions_NoError(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{}
	err := r.RegisterToToolkitWithOptions("core", &tools.ToolDeclaration{Name: "t"}, nil, tools.ToolOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanicRegistry_GetCoreDeclarations_NoPanic(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnGet: false}
	decls := r.GetCoreDeclarations()
	if len(decls) != 1 {
		t.Fatalf("got %d declarations; want 1", len(decls))
	}
	if decls[0].Name != "any" {
		t.Errorf("got name %q; want %q", decls[0].Name, "any")
	}
}

func TestPanicRegistry_GetCoreDeclarations_Panics(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnGet: true}
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic, but none occurred")
		}
		msg, ok := recovered.(string)
		if !ok || msg != "registry GetDeclarations panic" {
			t.Errorf("got panic %v; want 'registry GetDeclarations panic'", recovered)
		}
	}()
	r.GetCoreDeclarations()
}

func TestPanicRegistry_GetDeclarationsByToolkits_NoPanic(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnGet: false}
	decls := r.GetDeclarationsByToolkits([]string{"core"})
	if len(decls) != 1 {
		t.Fatalf("got %d declarations; want 1", len(decls))
	}
	if decls[0].Name != "any" {
		t.Errorf("got name %q; want %q", decls[0].Name, "any")
	}
}

func TestPanicRegistry_GetDeclarationsByToolkits_Panics(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnGet: true}
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic, but none occurred")
		}
		msg, ok := recovered.(string)
		if !ok || msg != "registry GetDeclarations panic" {
			t.Errorf("got panic %v; want 'registry GetDeclarations panic'", recovered)
		}
	}()
	r.GetDeclarationsByToolkits([]string{"core"})
}

func TestPanicRegistry_ListAvailableToolkits(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{}
	names := r.ListAvailableToolkits()
	if len(names) != 1 {
		t.Fatalf("got %d toolkits; want 1", len(names))
	}
	if names[0] != "core" {
		t.Errorf("got name %q; want %q", names[0], "core")
	}
}
