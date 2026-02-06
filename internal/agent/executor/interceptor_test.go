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

		expected := "Did you mean \"list_files\"?"
		if !strings.Contains(res.Text, expected) {
			t.Errorf("Expected suggestion %q, got: %s", expected, res.Text)
		}
	})

	t.Run("Case Insensitive Suggestion", func(t *testing.T) {
		res := exec.executeTool(context.Background(), &llm.FunctionCall{Name: "LIST_FILE"})

		expected := "Did you mean \"list_files\"?"
		if !strings.Contains(res.Text, expected) {
			t.Errorf("Expected case-insensitive suggestion %q, got: %s", expected, res.Text)
		}
	})
}

func TestToolExecutor_SuggestTool(t *testing.T) {
	exec := &ToolExecutor{}
	validTools := []string{"list_files", "read_file", "write_file", "git_status", "ls"}

	tests := []struct {
		hallucinated string
		expected     string
	}{
		{"list_file", "list_files"}, // dist 1, len 9 -> threshold 3
		{"LIST_FILE", "list_files"}, // case insensitive
		{"read_fil", "read_file"},   // dist 1
		{"rit_file", "write_file"},  // dist 2 (w missing, r instead of w?) actually write_file vs rit_file: w, r, i, t, e vs r, i, t. dist 2.
		{"git_stat", "git_status"},  // dist 2
		{"lx", "ls"},                // dist 1, len 2 -> threshold 1
		{"something_else", ""},      // dist high
		{"get_all_outputs", ""},     // dist high
	}

	for _, tt := range tests {
		t.Run(tt.hallucinated, func(t *testing.T) {
			got := exec.suggestTool(tt.hallucinated, validTools)
			if got != tt.expected {
				t.Errorf("suggestTool(%q) = %q, want %q", tt.hallucinated, got, tt.expected)
			}
		})
	}
}

func TestToolExecutor_MixedExecution(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{
		Name: "valid_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "Success"}, nil
	})

	sm := &mockSecurityManager{allowAll: true}
	exec := NewToolExecutor(reg, sm, nil)

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "valid_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "misspelled_tol"}},
			{FunctionCall: &llm.FunctionCall{Name: "completely_unknown"}},
		},
	}

	resultContent, err := exec.Execute(context.Background(), respContent, 0, 5)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(resultContent.Parts) != 3 {
		t.Fatalf("Expected 3 response parts, got %d", len(resultContent.Parts))
	}

	// Part 0: Valid tool
	if !strings.Contains(resultContent.Parts[0].FunctionResponse.Response["result"].(string), "Success") {
		t.Errorf("Part 0 expected Success, got: %v", resultContent.Parts[0].FunctionResponse.Response)
	}

	// Part 1: Misspelled tool (Interceptor should have worked)
	res1 := resultContent.Parts[1].FunctionResponse.Response["result"].(string)
	if !strings.Contains(res1, "Error: Tool \"misspelled_tol\" is not defined") {
		t.Errorf("Part 1 expected Interceptor error, got: %s", res1)
	}

	// Part 2: Unknown tool
	res2 := resultContent.Parts[2].FunctionResponse.Response["result"].(string)
	if !strings.Contains(res2, "Error: Tool \"completely_unknown\" is not defined") {
		t.Errorf("Part 2 expected Interceptor error, got: %s", res2)
	}
}

func TestWorkerPool_SubmitFailure(t *testing.T) {
	p := NewWorkerPool(1)
	p.Shutdown() // Close it immediately

	success := p.Submit(func(ctx context.Context) {})
	if success {
		t.Error("Expected Submit to fail on closed pool")
	}
}

func TestLevenshteinDistance_UTF8(t *testing.T) {
	tests := []struct {
		s1, s2 string
		want   int
	}{
		{"gopher", "go", 4},
		{"😊", "😊", 0},
		{"😊", "😂", 1},
		{"café", "cafe", 1},
		{"日本語", "日本", 1},
	}

	for _, tt := range tests {
		got := levenshteinDistance(tt.s1, tt.s2)
		if got != tt.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.want)
		}
	}
}
