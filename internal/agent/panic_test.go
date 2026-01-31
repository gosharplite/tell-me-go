// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestAgent_ExecuteToolsConcurrently_PanicRecovery(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&types.ToolDeclaration{
		Name: "panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		panic("intentional parallel panic")
	})
	registry.RegisterWithOptions(&types.ToolDeclaration{
		Name: "serial_panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		panic("intentional serial panic")
	}, tools.ToolOptions{Serial: true})

	sm := tools.NewSecurityManager()
	a := New(nil, nil, registry, sm)
	a.maxConcurrentTools = 2

	t.Run("Parallel Panic", func(t *testing.T) {
		calls := []*types.FunctionCall{
			{Name: "panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		a.executeToolsConcurrentStream(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional parallel panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})

	t.Run("Serial Panic", func(t *testing.T) {
		calls := []*types.FunctionCall{
			{Name: "serial_panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		a.executeToolsConcurrentStream(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional serial panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})
}
