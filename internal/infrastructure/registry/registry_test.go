// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package registry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/stretchr/testify/assert"
)

func TestRegistry_Resilience(t *testing.T) {
	t.Parallel()
	t.Run("error on empty tool name", func(t *testing.T) {
		t.Parallel()
		r := registry.New()
		err := r.Register(&tools.ToolDeclaration{Name: ""}, nil)
		if err == nil {
			t.Errorf("expected error for empty tool name, got nil")
		}
		if !strings.Contains(err.Error(), "cannot register tool with empty name") {
			t.Errorf("expected 'cannot register tool with empty name' error, got: %v", err)
		}
	})

	t.Run("error on unknown tool execution", func(t *testing.T) {
		t.Parallel()
		r := registry.New()
		_, err := r.Execute(context.Background(), "unknown", nil, nil)
		if err == nil {
			t.Fatal("expected error for unknown tool, got nil")
		}
		if !strings.Contains(err.Error(), "tool not found") {
			t.Errorf("expected 'tool not found' error, got: %v", err)
		}
	})

	t.Run("wrapped error on handler failure", func(t *testing.T) {
		t.Parallel()
		r := registry.New()
		targetErr := errors.New("something went wrong")
		if err := r.Register(&tools.ToolDeclaration{Name: "failer"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, targetErr
		}); err != nil {
			t.Fatalf("failed to register tool: %v", err)
		}

		_, err := r.Execute(context.Background(), "failer", nil, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, targetErr) {
			t.Errorf("expected wrapped error to contain targetErr, got: %v", err)
		}
		if !strings.Contains(err.Error(), "tool execution failed: failer") {
			t.Errorf("expected error message to contain tool name and failure context, got: %v", err)
		}
	})
}

func TestRegistry_Options(t *testing.T) {
	t.Parallel()
	r := registry.New()
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{Name: "serial_tool"}, nil, registry.ToolOptions{Serial: true}); err != nil {
		t.Fatalf("failed to register tool serial_tool: %v", err)
	}
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{Name: "long_running_tool"}, nil, registry.ToolOptions{LongRunning: true}); err != nil {
		t.Fatalf("failed to register tool long_running_tool: %v", err)
	}

	if !r.IsSerial("serial_tool") {
		t.Error("expected serial_tool to be serial")
	}
	if r.IsSerial("long_running_tool") {
		t.Error("expected long_running_tool not to be serial")
	}
	if r.IsSerial("nonexistent") {
		t.Error("expected nonexistent tool not to be serial")
	}

	if !r.IsLongRunning("long_running_tool") {
		t.Error("expected long_running_tool to be long running")
	}
	if r.IsLongRunning("serial_tool") {
		t.Error("expected serial_tool not to be long running")
	}
	if r.IsLongRunning("nonexistent") {
		t.Error("expected nonexistent tool not to be long running")
	}
}

func TestRegistry_GetDeclarations(t *testing.T) {
	t.Parallel()
	r := registry.New()
	if err := r.Register(&tools.ToolDeclaration{Name: "tool1"}, nil); err != nil {
		t.Fatalf("failed to register tool1: %v", err)
	}
	if err := r.Register(&tools.ToolDeclaration{Name: "tool2"}, nil); err != nil {
		t.Fatalf("failed to register tool2: %v", err)
	}

	decls := r.GetDeclarations()
	if len(decls) != 2 {
		t.Errorf("expected 2 declarations, got %d", len(decls))
	}
}

func TestRegistry_Toolkits(t *testing.T) {
	t.Parallel()
	r := registry.New()

	// Register some tools in different toolkits
	_ = r.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "core1"}, nil)
	_ = r.RegisterToToolkit("git", &tools.ToolDeclaration{Name: "git1"}, nil)
	_ = r.RegisterToToolkit("git", &tools.ToolDeclaration{Name: "git2"}, nil)
	_ = r.RegisterToToolkit("k8s", &tools.ToolDeclaration{Name: "k8s1"}, nil)

	t.Run("GetCoreDeclarations", func(t *testing.T) {
		decls := r.GetCoreDeclarations()
		assert.Len(t, decls, 1)
		assert.Equal(t, "core1", decls[0].Name)
	})

	t.Run("GetDeclarationsByToolkits - only core", func(t *testing.T) {
		decls := r.GetDeclarationsByToolkits(nil)
		assert.Len(t, decls, 1)
		assert.Equal(t, "core1", decls[0].Name)
	})

	t.Run("GetDeclarationsByToolkits - core + git", func(t *testing.T) {
		decls := r.GetDeclarationsByToolkits([]string{"git"})
		assert.Len(t, decls, 3)

		names := make(map[string]bool)
		for _, d := range decls {
			names[d.Name] = true
		}
		assert.True(t, names["core1"])
		assert.True(t, names["git1"])
		assert.True(t, names["git2"])
		assert.False(t, names["k8s1"])
	})

	t.Run("ListAvailableToolkits", func(t *testing.T) {
		toolkits := r.ListAvailableToolkits()
		assert.Len(t, toolkits, 3)

		tks := make(map[string]bool)
		for _, tk := range toolkits {
			tks[tk] = true
		}
		assert.True(t, tks["core"])
		assert.True(t, tks["git"])
		assert.True(t, tks["k8s"])
	})
}

func TestRegistry_RegisterToToolkitWithOptions(t *testing.T) {
	t.Parallel()
	r := registry.New()

	err := r.RegisterToToolkitWithOptions("git", &tools.ToolDeclaration{Name: "git_serial"}, nil, tools.ToolOptions{Serial: true})
	assert.NoError(t, err)

	assert.True(t, r.IsSerial("git_serial"))

	decls := r.GetDeclarationsByToolkits([]string{"git"})
	found := false
	for _, d := range decls {
		if d.Name == "git_serial" {
			found = true
			break
		}
	}
	assert.True(t, found, "git_serial should be in git toolkit")
}
