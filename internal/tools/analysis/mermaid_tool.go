// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// generateMermaidDiagram transforms a dependency map into a Mermaid.js diagram.
// It is registered as a tool.
func generateMermaidDiagram(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	rawGraph, ok := args["graph"]
	if !ok {
		return tools.ToolResult{Text: "Error: missing 'graph' argument"}, nil
	}

	graph := make(map[string][]string)
	switch m := rawGraph.(type) {
	case map[string][]string:
		graph = m
	case map[string]interface{}:
		for k, v := range m {
			switch deps := v.(type) {
			case []string:
				graph[k] = deps
			case []interface{}:
				for _, d := range deps {
					if s, ok := d.(string); ok {
						graph[k] = append(graph[k], s)
					}
				}
			}
		}
	default:
		return tools.ToolResult{Text: "Error: 'graph' argument must be a map of string to string list"}, nil
	}

	return tools.ToolResult{Text: generateMermaid(graph)}, nil
}
