// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/code/analysis"
)

// GenerateMermaidDiagram transforms a dependency map into a Mermaid.js diagram.
// It is registered as a tool.
func GenerateMermaidDiagram(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var graph map[string][]string
	
	rawGraph, ok := args["graph"]
	if !ok {
		return tools.ToolResult{Text: "Error: missing 'graph' argument"}, nil
	}

	// Manual conversion from interface{} to map[string][]string
	graph = make(map[string][]string)
	if m, ok := rawGraph.(map[string]interface{}); ok {
		for k, v := range m {
			if deps, ok := v.([]interface{}); ok {
				var depStrings []string
				for _, d := range deps {
					if s, ok := d.(string); ok {
						depStrings = append(depStrings, s)
					}
				}
				graph[k] = depStrings
			} else if deps, ok := v.([]string); ok {
				graph[k] = deps
			}
		}
	} else if m, ok := rawGraph.(map[string][]string); ok {
		graph = m
	} else {
		return tools.ToolResult{Text: "Error: 'graph' argument must be a map of string to string list"}, nil
	}

	return tools.ToolResult{Text: analysis.GenerateMermaid(graph)}, nil
}

