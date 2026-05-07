// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// coerceGraphMap converts a map[string]interface{} to map[string][]string
// by coercing each value that is []string or []interface{}.
func coerceGraphMap(m map[string]interface{}) map[string][]string {
	graph := make(map[string][]string)
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
	return graph
}

// normalizeGraphArg coerces the raw graph argument into a map[string][]string.
// It handles three cases:
//   - nil: returns an error about the missing graph argument
//   - map[string][]string: returned as-is
//   - map[string]interface{}: delegated to coerceGraphMap
//   - anything else: returns a type error
func normalizeGraphArg(rawGraph interface{}) (map[string][]string, error) {
	if rawGraph == nil {
		return nil, errors.New("missing 'graph' argument")
	}

	var graph map[string][]string
	switch m := rawGraph.(type) {
	case map[string][]string:
		graph = m
	case map[string]interface{}:
		graph = coerceGraphMap(m)
	default:
		return nil, errors.New("'graph' argument must be a map of string to string list")
	}

	return graph, nil
}

// generateMermaidDiagram transforms a dependency map into a Mermaid.js diagram.
// It is registered as a tool.
func generateMermaidDiagram(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	graph, err := normalizeGraphArg(args["graph"])
	if err != nil {
		return tools.ToolResult{Text: "Error: " + err.Error()}, nil
	}
	return tools.ToolResult{Text: generateMermaid(graph)}, nil
}
