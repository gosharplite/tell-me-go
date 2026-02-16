// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// resultStrategy defines how tool outputs are transformed back into LLM messages.
type resultStrategy = services.ResultStrategy

// markdownStrategy formats tool results as markdown-friendly text.
type markdownStrategy struct{}

func (s *markdownStrategy) Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part {
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			ID:       call.ID,
			Name:     call.Name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

// jsonStrategy formats tool results as raw JSON.
type jsonStrategy struct{}

func (s *jsonStrategy) Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part {
	// For now it's similar to markdownStrategy but could differ in the future
	// (e.g. returning structured data instead of just a 'result' string field).
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			ID:       call.ID,
			Name:     call.Name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}
