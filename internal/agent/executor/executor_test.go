// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestToolExecutor_PanicRecovery(t *testing.T) {
	reg := registry.New()
	reg.Register(&types.ToolDeclaration{
		Name: "panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		panic("intentional parallel panic")
	})
	reg.RegisterWithOptions(&types.ToolDeclaration{
		Name: "serial_panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		panic("intentional serial panic")
	}, tools.ToolOptions{Serial: true})

	sm := tools.NewSecurityManager()
	bus := &events.SimpleEventBus{}
	exec := NewToolExecutor(reg, sm, bus)
	exec.SetConcurrency(2, 0)

	t.Run("Parallel Panic", func(t *testing.T) {
		calls := []*types.FunctionCall{
			{Name: "panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		exec.executeToolsConcurrentStream(context.Background(), calls, resChan)

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
		exec.executeToolsConcurrentStream(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional serial panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})
}
