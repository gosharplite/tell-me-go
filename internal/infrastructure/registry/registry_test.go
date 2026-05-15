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

	t.Run("GetDeclarationsByToolkits - core explicitly requested", func(t *testing.T) {
		// Explicitly requesting "core" should not double-add core tools.
		decls := r.GetDeclarationsByToolkits([]string{"core"})
		assert.Len(t, decls, 1)
		assert.Equal(t, "core1", decls[0].Name)

		// Requesting "core" alongside "git" should still dedupe correctly.
		decls = r.GetDeclarationsByToolkits([]string{"core", "git"})
		assert.Len(t, decls, 3)

		freq := make(map[string]int)
		for _, d := range decls {
			freq[d.Name]++
		}
		for name, count := range freq {
			assert.Equal(t, 1, count, "tool %q appeared %d times; expected exactly once", name, count)
		}
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

func TestRegistry_GetOptions(t *testing.T) {
	t.Parallel()
	r := registry.New()

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{Name: "serial_tool"}, nil, registry.ToolOptions{Serial: true}); err != nil {
		t.Fatalf("failed to register serial_tool: %v", err)
	}
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{Name: "long_tool"}, nil, registry.ToolOptions{LongRunning: true}); err != nil {
		t.Fatalf("failed to register long_tool: %v", err)
	}
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{Name: "both_tool"}, nil, registry.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		t.Fatalf("failed to register both_tool: %v", err)
	}

	t.Run("serial tool", func(t *testing.T) {
		t.Parallel()
		got := r.GetOptions("serial_tool")
		if !got.Serial {
			t.Errorf("expected serial_tool to have Serial=true, got %+v", got)
		}
	})

	t.Run("long-running tool", func(t *testing.T) {
		t.Parallel()
		got := r.GetOptions("long_tool")
		if !got.LongRunning {
			t.Errorf("expected long_tool to have LongRunning=true, got %+v", got)
		}
	})

	t.Run("tool with both options", func(t *testing.T) {
		t.Parallel()
		got := r.GetOptions("both_tool")
		if !got.Serial {
			t.Errorf("expected both_tool to have Serial=true, got %+v", got)
		}
		if !got.LongRunning {
			t.Errorf("expected both_tool to have LongRunning=true, got %+v", got)
		}
	})

	t.Run("nonexistent tool", func(t *testing.T) {
		t.Parallel()
		got := r.GetOptions("no_such_tool")
		if got != (registry.ToolOptions{}) {
			t.Errorf("expected zero-value ToolOptions for nonexistent tool, got %+v", got)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		got := r.GetOptions("")
		if got != (registry.ToolOptions{}) {
			t.Errorf("expected zero-value ToolOptions for empty name, got %+v", got)
		}
	})
}

func TestRegistry_RegisterToToolkit_EmptyDefaultsToCore(t *testing.T) {
	t.Parallel()
	r := registry.New()

	t.Run("RegisterToToolkit(\"\", def, handler) succeeds", func(t *testing.T) {
		err := r.RegisterToToolkit("", &tools.ToolDeclaration{Name: "empty_tk_tool"}, nil)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		decls := r.GetCoreDeclarations()
		found := false
		for _, d := range decls {
			if d.Name == "empty_tk_tool" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'empty_tk_tool' in core declarations")
		}
	})

	t.Run("RegisterToToolkitWithOptions(\"\", def, handler, opts) also defaults to core", func(t *testing.T) {
		err := r.RegisterToToolkitWithOptions("", &tools.ToolDeclaration{Name: "empty_tk_serial"}, nil, registry.ToolOptions{Serial: true})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !r.IsSerial("empty_tk_serial") {
			t.Error("expected empty_tk_serial to be serial")
		}

		decls := r.GetCoreDeclarations()
		found := false
		for _, d := range decls {
			if d.Name == "empty_tk_serial" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'empty_tk_serial' in core declarations")
		}

		toolkits := r.ListAvailableToolkits()
		for _, tk := range toolkits {
			if tk == "" {
				t.Errorf("expected empty string not to appear in available toolkits, got: %v", toolkits)
			}
		}
	})

	t.Run("multiple empty-toolkit registrations all go to core", func(t *testing.T) {
		for _, name := range []string{"multi1", "multi2", "multi3"} {
			if err := r.RegisterToToolkit("", &tools.ToolDeclaration{Name: name}, nil); err != nil {
				t.Fatalf("failed to register %s: %v", name, err)
			}
		}

		decls := r.GetCoreDeclarations()
		// Expect 5 total: empty_tk_tool + empty_tk_serial + multi1 + multi2 + multi3
		if len(decls) != 5 {
			t.Errorf("expected 5 declarations, got %d", len(decls))
		}

		names := make(map[string]bool)
		for _, d := range decls {
			names[d.Name] = true
		}
		for _, name := range []string{"multi1", "multi2", "multi3"} {
			if !names[name] {
				t.Errorf("expected %q in core declarations", name)
			}
		}

		toolkits := r.ListAvailableToolkits()
		for _, tk := range toolkits {
			if tk == "" {
				t.Errorf("expected empty string not to appear in available toolkits, got: %v", toolkits)
			}
		}
	})
}
