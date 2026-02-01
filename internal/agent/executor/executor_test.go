// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestToolExecutor_PanicRecovery(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{
		Name: "panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		panic("intentional parallel panic")
	})
	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "serial_panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		panic("intentional serial panic")
	}, registry.ToolOptions{Serial: true})

	sm := security.NewSecurityManager(nil)
	bus := &events.SimpleEventBus{}
	exec := NewToolExecutor(reg, sm, bus)
	exec.SetConcurrency(2, 0)

	t.Run("Parallel Panic", func(t *testing.T) {
		calls := []*llm.FunctionCall{
			{Name: "panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		exec.runExecutionPlan(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional parallel panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})

	t.Run("Serial Panic", func(t *testing.T) {
		calls := []*llm.FunctionCall{
			{Name: "serial_panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		exec.runExecutionPlan(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional serial panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})
}
