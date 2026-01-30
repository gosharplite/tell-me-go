// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

func TestRegistry_SerialProperty(t *testing.T) {
	r := NewRegistry()

	// 1. Default registration (Parallel)
	r.Register(&genai.FunctionDeclaration{Name: "parallel_tool"}, nil)
	if r.IsSerial("parallel_tool") {
		t.Error("expected parallel_tool to be parallel (Serial: false)")
	}

	// 2. Explicit Serial registration
	r.RegisterWithOptions(&genai.FunctionDeclaration{Name: "serial_tool"}, nil, ToolOptions{Serial: true})
	if !r.IsSerial("serial_tool") {
		t.Error("expected serial_tool to be serial (Serial: true)")
	}

	// 3. Explicit Parallel registration via options
	r.RegisterWithOptions(&genai.FunctionDeclaration{Name: "parallel_opt_tool"}, nil, ToolOptions{Serial: false})
	if r.IsSerial("parallel_opt_tool") {
		t.Error("expected parallel_opt_tool to be parallel")
	}

	// 4. Non-existent tool
	if r.IsSerial("unknown") {
		t.Error("expected unknown tool to return Serial: false")
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry()
	ctx := context.Background()
	r.Register(&genai.FunctionDeclaration{Name: "test"}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "success", nil
	})

	res, err := r.Execute(ctx, "test", nil)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
	if res != "success" {
		t.Errorf("expected success, got %s", res)
	}

	_, err = r.Execute(ctx, "unknown", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}
