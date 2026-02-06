// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestStrategies(t *testing.T) {
	tests := []struct {
		name     string
		strategy ResultStrategy
		toolName string
		result   tools.ToolResult
		wantText string
	}{
		{
			name:     "Markdown - Basic Text",
			strategy: &MarkdownStrategy{},
			toolName: "ls",
			result:   tools.ToolResult{Text: "file.txt"},
			wantText: "file.txt",
		},
		{
			name:     "JSON - Basic Text",
			strategy: &JSONStrategy{},
			toolName: "ls",
			result:   tools.ToolResult{Text: "file.txt"},
			wantText: "file.txt",
		},
		{
			name:     "Empty Result",
			strategy: &MarkdownStrategy{},
			toolName: "cmd",
			result:   tools.ToolResult{Text: ""},
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			part := tt.strategy.Format(tt.toolName, tt.result)
			if part.FunctionResponse.Name != tt.toolName {
				t.Errorf("Expected name %s, got %s", tt.toolName, part.FunctionResponse.Name)
			}
			gotText := part.FunctionResponse.Response["result"].(string)
			if gotText != tt.wantText {
				t.Errorf("Expected text %q, got %q", tt.wantText, gotText)
			}
		})
	}
}
