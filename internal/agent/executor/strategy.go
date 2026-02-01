// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ResultStrategy defines how tool outputs are transformed back into LLM messages.
type ResultStrategy interface {
	Format(name string, result tools.ToolResult) *llm.Part
}

// MarkdownStrategy formats tool results as markdown-friendly text.
type MarkdownStrategy struct{}

func (s *MarkdownStrategy) Format(name string, result tools.ToolResult) *llm.Part {
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

// JSONStrategy formats tool results as raw JSON.
type JSONStrategy struct{}

func (s *JSONStrategy) Format(name string, result tools.ToolResult) *llm.Part {
	// For now it's similar to MarkdownStrategy but could differ in the future
	// (e.g. returning structured data instead of just a 'result' string field).
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}
