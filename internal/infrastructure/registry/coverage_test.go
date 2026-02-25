// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package registry_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

func TestRegistry_DuplicateRegistration(t *testing.T) {
	tests := []struct {
		name     string
		actions  func(r tools.IToolRegistry)
		validate func(t *testing.T, r tools.IToolRegistry)
	}{
		{
			name: "update existing tool",
			actions: func(r tools.IToolRegistry) {
				def1 := &tools.ToolDeclaration{Name: "tool1", Description: "desc1"}
				h1 := func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "v1"}, nil
				}
				r.Register(def1, h1)

				def2 := &tools.ToolDeclaration{Name: "tool1", Description: "desc2"}
				h2 := func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "v2"}, nil
				}
				r.RegisterWithOptions(def2, h2, registry.ToolOptions{Serial: true})
			},
			validate: func(t *testing.T, r tools.IToolRegistry) {
				decls := r.GetDeclarations()
				if len(decls) != 1 {
					t.Errorf("expected 1 declaration, got %d", len(decls))
				}
				if decls[0].Description != "desc2" {
					t.Errorf("expected updated description 'desc2', got '%s'", decls[0].Description)
				}
				if !r.IsSerial("tool1") {
					t.Errorf("expected tool1 to be serial after update")
				}
				res, _ := r.Execute(context.Background(), "tool1", nil)
				if res.Text != "v2" {
					t.Errorf("expected updated handler to return 'v2', got '%s'", res.Text)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := registry.New()
			tt.actions(r)
			tt.validate(t, r)
		})
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	tests := []struct {
		name        string
		numRoutines int
	}{
		{"high concurrency", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := registry.New()
			var wg sync.WaitGroup

			// Start goroutines that register and execute tools concurrently
			for i := 0; i < tt.numRoutines; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					name := fmt.Sprintf("tool-%d", id)
					def := &tools.ToolDeclaration{Name: name}
					handler := func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
						return tools.ToolResult{Text: fmt.Sprintf("res-%d", id)}, nil
					}

					reg.Register(def, handler)

					// Also try to update some existing ones
					if id > 0 {
						prevName := fmt.Sprintf("tool-%d", id-1)
						_ = reg.IsSerial(prevName)
						_ = reg.IsLongRunning(prevName)
						_, _ = reg.Execute(context.Background(), prevName, nil)
					}

					_, _ = reg.Execute(context.Background(), name, nil)
					_ = reg.GetDeclarations()
				}(i)
			}
			wg.Wait()
		})
	}
}
