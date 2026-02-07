// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package registry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestRegistry_Resilience(t *testing.T) {
	t.Run("panic on empty tool name", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic")
			} else {
				errMsg := r.(string)
				if errMsg != "cannot register tool with empty name" {
					t.Errorf("Unexpected panic message: %s", errMsg)
				}
			}
		}()

		r := registry.New()
		r.Register(&tools.ToolDeclaration{Name: ""}, nil)
	})

	t.Run("error on unknown tool execution", func(t *testing.T) {
		r := registry.New()
		_, err := r.Execute(context.Background(), "unknown", nil)
		if err == nil {
			t.Fatal("expected error for unknown tool, got nil")
		}
		if !strings.Contains(err.Error(), "tool not found") {
			t.Errorf("expected 'tool not found' error, got: %v", err)
		}
	})

	t.Run("wrapped error on handler failure", func(t *testing.T) {
		r := registry.New()
		targetErr := errors.New("something went wrong")
		r.Register(&tools.ToolDeclaration{Name: "failer"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, targetErr
		})

		_, err := r.Execute(context.Background(), "failer", nil)
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
	r := registry.New()
	r.RegisterWithOptions(&tools.ToolDeclaration{Name: "serial_tool"}, nil, registry.ToolOptions{Serial: true})
	r.RegisterWithOptions(&tools.ToolDeclaration{Name: "long_running_tool"}, nil, registry.ToolOptions{LongRunning: true})

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
	r := registry.New()
	r.Register(&tools.ToolDeclaration{Name: "tool1"}, nil)
	r.Register(&tools.ToolDeclaration{Name: "tool2"}, nil)

	decls := r.GetDeclarations()
	if len(decls) != 2 {
		t.Errorf("expected 2 declarations, got %d", len(decls))
	}
}

func TestRegistry_UnmarshalArgs(t *testing.T) {
	type Args struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	args := map[string]interface{}{"name": "Alice", "age": 30}
	var target Args
	err := registry.UnmarshalArgs(args, &target)
	if err != nil {
		t.Fatalf("UnmarshalArgs failed: %v", err)
	}
	if target.Name != "Alice" || target.Age != 30 {
		t.Errorf("unexpected unmarshaled values: %+v", target)
	}
}
