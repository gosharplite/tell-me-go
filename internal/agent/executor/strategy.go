// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// resultStrategy defines how tool outputs are transformed back into LLM messages.
type resultStrategy interface {
	// Format converts a tool execution result into an LLM-compatible Part.
	// The call parameter provides the original function invocation context;
	// the result parameter contains the tool's output.
	Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part
}

// markdownStrategy formats tool results as markdown-friendly text.
type markdownStrategy struct{}

func (s *markdownStrategy) Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part {
	return buildFunctionResponse(call.ID, call.Name, result.Text)
}
