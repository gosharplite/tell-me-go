// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

func TestExecutionAdapter_Execute(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{Name: "test_tool"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "success"}, nil
	})

	// Use existing mockSecurityManager from shared_test.go
	ex, err := NewToolExecutor(reg, &mockSecurityManager{allowAll: true}, nil, &MockLogger{CriticalLogs: make(chan string, 10)})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}
	defer ex.Shutdown()

	adapter := NewExecutionAdapter(ex)

	t.Run("successful execution", func(t *testing.T) {
		content := &llm.Content{
			Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
			},
		}
		resp, err := adapter.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response, got nil")
		}
	})

	t.Run("executor not initialized", func(t *testing.T) {
		nilAdapter := NewExecutionAdapter(nil)
		_, err := nilAdapter.Execute(context.Background(), nil, 0, 10)
		if err == nil {
			t.Fatal("expected error for nil executor, got nil")
		}
		if err.Error() != "executor not initialized" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
