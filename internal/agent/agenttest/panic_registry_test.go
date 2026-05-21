// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"
)

func TestPanicRegistry_GetDeclarations_Normal(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnGet: false}
	decls := r.GetDeclarations()
	if len(decls) != 1 {
		t.Fatalf("got %d declarations; want 1", len(decls))
	}
	if decls[0].Name != "any" {
		t.Errorf("got name %q; want %q", decls[0].Name, "any")
	}
}

func TestPanicRegistry_GetDeclarations_Panics(t *testing.T) {
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
	r.GetDeclarations()
}

func TestPanicRegistry_Execute_Normal(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnExec: false}
	result, err := r.Execute(context.Background(), "any", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Text != "" {
		t.Errorf("got Text %q; want empty", result.Text)
	}
}

func TestPanicRegistry_Execute_Panics(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{PanicOnExec: true}
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic, but none occurred")
		}
		msg, ok := recovered.(string)
		if !ok || msg != "registry Execute panic" {
			t.Errorf("got panic %v; want 'registry Execute panic'", recovered)
		}
	}()
	_, _ = r.Execute(context.Background(), "any", nil, nil)
}

func TestPanicRegistry_IsSerial(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{Serial: true}
	if !r.IsSerial("any") {
		t.Error("expected IsSerial=true")
	}
}

func TestPanicRegistry_IsLongRunning(t *testing.T) {
	t.Parallel()

	r := &PanicRegistry{}
	if r.IsLongRunning("any") {
		t.Error("expected IsLongRunning=false")
	}
}
