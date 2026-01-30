// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

func TestAgent_ExecuteToolsConcurrently_PanicRecovery(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&genai.FunctionDeclaration{
		Name: "panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		panic("intentional parallel panic")
	})
	registry.RegisterWithOptions(&genai.FunctionDeclaration{
		Name: "serial_panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		panic("intentional serial panic")
	}, tools.ToolOptions{Serial: true})

	sm := tools.NewSecurityManager()
	a := New(nil, nil, registry, sm)
	a.maxConcurrentTools = 2

	t.Run("Parallel Panic", func(t *testing.T) {
		calls := []*types.FunctionCall{
			{Name: "panic_tool"},
		}

		results := a.executeToolsConcurrently(context.Background(), calls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0].FunctionResponse.Response["result"].(string)
		if !strings.Contains(res, "Panic detected: intentional parallel panic") {
			t.Errorf("expected panic error message, got: %s", res)
		}
	})

	t.Run("Serial Panic", func(t *testing.T) {
		calls := []*types.FunctionCall{
			{Name: "serial_panic_tool"},
		}

		results := a.executeToolsConcurrently(context.Background(), calls)

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0].FunctionResponse.Response["result"].(string)
		if !strings.Contains(res, "Panic detected: intentional serial panic") {
			t.Errorf("expected panic error message, got: %s", res)
		}
	})
}
