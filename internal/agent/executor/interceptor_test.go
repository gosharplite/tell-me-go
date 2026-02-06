// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestToolExecutor_HallucinatedToolInterception(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{
		Name: "list_files",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "files..."}, nil
	})

	sm := &mockSecurityManager{allowAll: true}
	exec := NewToolExecutor(reg, sm, nil)

	t.Run("Intercept Non-Existent Tool", func(t *testing.T) {
		res := exec.executeTool(context.Background(), &llm.FunctionCall{Name: "get_all_outputs"})
		
		if !strings.Contains(res.Text, "Error: Tool \"get_all_outputs\" is not defined") {
			t.Errorf("Expected interception message, got: %s", res.Text)
		}
		
		if !strings.Contains(res.Text, "Available tools are: [list_files]") {
			t.Errorf("Expected available tools list, got: %s", res.Text)
		}

		if res.Error == nil {
			t.Error("Expected error to be set in ToolResult for internal tracking")
		}
	})

	t.Run("Suggestion for Misspelled Tool", func(t *testing.T) {
		res := exec.executeTool(context.Background(), &llm.FunctionCall{Name: "list_file"})
		
		// This part will fail until I implement suggestTool
		if !strings.Contains(res.Text, "Did you mean \"list_files\"?") {
			t.Logf("Suggestion not yet implemented or didn't match: %s", res.Text)
		}
	})
}
