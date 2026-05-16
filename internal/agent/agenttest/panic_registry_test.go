// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"
)

func TestPanicRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *PanicRegistry
		check func(t *testing.T, r *PanicRegistry)
	}{
		{
			name: "GetDeclarations_normal",
			setup: func() *PanicRegistry {
				return &PanicRegistry{PanicOnGet: false}
			},
			check: func(t *testing.T, r *PanicRegistry) {
				decls := r.GetDeclarations()
				if len(decls) != 1 {
					t.Fatalf("got %d declarations; want 1", len(decls))
				}
				if decls[0].Name != "any" {
					t.Errorf("got name %q; want %q", decls[0].Name, "any")
				}
			},
		},
		{
			name: "GetDeclarations_panics",
			setup: func() *PanicRegistry {
				return &PanicRegistry{PanicOnGet: true}
			},
			check: func(t *testing.T, r *PanicRegistry) {
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
			},
		},
		{
			name: "Execute_normal",
			setup: func() *PanicRegistry {
				return &PanicRegistry{PanicOnExec: false}
			},
			check: func(t *testing.T, r *PanicRegistry) {
				result, err := r.Execute(context.Background(), "any", nil, nil)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result.Text != "" {
					t.Errorf("got Text %q; want empty", result.Text)
				}
			},
		},
		{
			name: "Execute_panics",
			setup: func() *PanicRegistry {
				return &PanicRegistry{PanicOnExec: true}
			},
			check: func(t *testing.T, r *PanicRegistry) {
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
			},
		},
		{
			name: "IsSerial",
			setup: func() *PanicRegistry {
				return &PanicRegistry{Serial: true}
			},
			check: func(t *testing.T, r *PanicRegistry) {
				if !r.IsSerial("any") {
					t.Error("expected IsSerial=true")
				}
			},
		},
		{
			name: "IsLongRunning",
			setup: func() *PanicRegistry {
				return &PanicRegistry{}
			},
			check: func(t *testing.T, r *PanicRegistry) {
				if r.IsLongRunning("any") {
					t.Error("expected IsLongRunning=false")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := tt.setup()
			tt.check(t, r)
		})
	}
}
