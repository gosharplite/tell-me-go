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
	t.Parallel()
	tests := []struct {
		name     string
		actions  func(t *testing.T, r tools.Registry)
		validate func(t *testing.T, r tools.Registry)
	}{
		{
			name: "update existing tool",
			actions: func(t *testing.T, r tools.Registry) {
				def1 := &tools.ToolDeclaration{Name: "tool1", Description: "desc1"}
				h1 := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "v1"}, nil
				}
				if err := r.Register(def1, h1); err != nil {
					t.Fatalf("failed to register tool: %v", err)
				}

				def2 := &tools.ToolDeclaration{Name: "tool1", Description: "desc2"}
				h2 := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "v2"}, nil
				}
				if err := r.RegisterWithOptions(def2, h2, registry.ToolOptions{Serial: true}); err != nil {
					t.Fatalf("failed to register tool with options: %v", err)
				}
			},
			validate: func(t *testing.T, r tools.Registry) {
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
				res, _ := r.Execute(context.Background(), "tool1", nil, nil)
				if res.Text != "v2" {
					t.Errorf("expected updated handler to return 'v2', got '%s'", res.Text)
				}
			},
		},
		{
			name: "re-register to different toolkit",
			actions: func(t *testing.T, r tools.Registry) {
				def1 := &tools.ToolDeclaration{Name: "cross_tool", Description: "desc_v1"}
				h1 := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "v1"}, nil
				}
				if err := r.RegisterToToolkit("core", def1, h1); err != nil {
					t.Fatalf("failed to register tool to core: %v", err)
				}

				def2 := &tools.ToolDeclaration{Name: "cross_tool", Description: "desc_v2"}
				h2 := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
					return tools.ToolResult{Text: "v2"}, nil
				}
				if err := r.RegisterToToolkitWithOptions("git", def2, h2, registry.ToolOptions{LongRunning: true}); err != nil {
					t.Fatalf("failed to re-register tool to git: %v", err)
				}
			},
			validate: func(t *testing.T, r tools.Registry) {
				// 1. cross_tool remains in core declarations
				coreDecls := r.GetCoreDeclarations()
				foundInCore := false
				for _, d := range coreDecls {
					if d.Name == "cross_tool" {
						foundInCore = true
						break
					}
				}
				if !foundInCore {
					t.Errorf("expected cross_tool to remain in core declarations")
				}

				// 2. cross_tool also appears in git toolkit declarations
				gitDecls := r.GetDeclarationsByToolkits([]string{"git"})
				foundInGit := false
				for _, d := range gitDecls {
					if d.Name == "cross_tool" {
						foundInGit = true
						break
					}
				}
				if !foundInGit {
					t.Errorf("expected cross_tool to appear in git toolkit declarations")
				}

				// 3. Only one entry (no duplication)
				decls := r.GetDeclarations()
				if len(decls) != 1 {
					t.Errorf("expected 1 declaration, got %d", len(decls))
				}

				// 4. Declaration was updated
				if len(decls) > 0 && decls[0].Description != "desc_v2" {
					t.Errorf("expected updated description 'desc_v2', got '%s'", decls[0].Description)
				}

				// 5. Options were updated (LongRunning enabled)
				if !r.IsLongRunning("cross_tool") {
					t.Errorf("expected cross_tool to be long-running after update")
				}

				// 6. Handler was updated
				res, _ := r.Execute(context.Background(), "cross_tool", nil, nil)
				if res.Text != "v2" {
					t.Errorf("expected updated handler to return 'v2', got '%s'", res.Text)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := registry.New()
			tt.actions(t, r)
			tt.validate(t, r)
		})
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		numRoutines int
	}{
		{"high concurrency", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg := registry.New()
			var wg sync.WaitGroup

			// Start goroutines that register and execute tools concurrently
			for i := 0; i < tt.numRoutines; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					name := fmt.Sprintf("tool-%d", id)
					def := &tools.ToolDeclaration{Name: name}
					handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
						return tools.ToolResult{Text: fmt.Sprintf("res-%d", id)}, nil
					}

					if err := reg.Register(def, handler); err != nil {
						t.Errorf("failed to register tool in concurrent routine: %v", err)
						return
					}

					// Also try to update some existing ones
					if id > 0 {
						prevName := fmt.Sprintf("tool-%d", id-1)
						_ = reg.IsSerial(prevName)
						_ = reg.IsLongRunning(prevName)
						_, _ = reg.Execute(context.Background(), prevName, nil, nil)
					}

					_, _ = reg.Execute(context.Background(), name, nil, nil)
					_ = reg.GetDeclarations()
				}(i)
			}
			wg.Wait()
		})
	}
}
