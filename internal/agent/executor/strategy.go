// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"github.com/gosharplite/tell-me-go/internal/types"
)

// ResultStrategy defines how tool outputs are transformed back into LLM messages.
type ResultStrategy interface {
	Format(name string, result types.ToolResult) *types.Part
}

// MarkdownStrategy formats tool results as markdown-friendly text.
type MarkdownStrategy struct{}

func (s *MarkdownStrategy) Format(name string, result types.ToolResult) *types.Part {
	return &types.Part{
		FunctionResponse: &types.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

// JSONStrategy formats tool results as raw JSON.
type JSONStrategy struct{}

func (s *JSONStrategy) Format(name string, result types.ToolResult) *types.Part {
	// For now it's similar to MarkdownStrategy but could differ in the future
	// (e.g. returning structured data instead of just a 'result' string field).
	return &types.Part{
		FunctionResponse: &types.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}
