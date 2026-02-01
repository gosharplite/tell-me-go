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

func TestResultCollector(t *testing.T) {
	t.Parallel()
	calls := []*llm.FunctionCall{
		{Name: "tool0"},
		{Name: "tool1"},
		{Name: "tool2"},
	}

	t.Run("Ordering", func(t *testing.T) {
		collector := newResultCollector(calls, nil)
		// Simulate tools finishing out of order
		collector.ch <- toolExecResult{index: 2, name: "tool2", tr: tools.ToolResult{Text: "res2"}}
		collector.ch <- toolExecResult{index: 0, name: "tool0", tr: tools.ToolResult{Text: "res0"}}
		collector.ch <- toolExecResult{index: 1, name: "tool1", tr: tools.ToolResult{Text: "res1"}}

		results, err := collector.Wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if len(results) != 3 {
			t.Fatalf("Expected 3 results, got %d", len(results))
		}
		if results[0].Text != "res0" || results[1].Text != "res1" || results[2].Text != "res2" {
			t.Errorf("Results out of order: %v", results)
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		collector := newResultCollector(calls, nil)
		cancel()

		_, err := collector.Wait(ctx)
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})
}
