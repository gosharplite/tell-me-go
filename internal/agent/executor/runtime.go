// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// baseRuntime implements the raw execution logic of tools.
type baseRuntime struct {
	registry tools.Registry
}

// newBaseRuntime creates a new baseRuntime.
func newBaseRuntime(registry tools.Registry) *baseRuntime {
	return &baseRuntime{
		registry: registry,
	}
}

// Execute performs the raw execution of a tool call.
func (b *baseRuntime) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (tools.ToolResult, error) {
	// 2. Unmarshal/validate arguments (already done by the LLM in call.Args)
	// 3. Execute the underlying tool logic
	// 4. Return the ToolResult
	return b.registry.Execute(ctx, tool.Name, call.Args)
}
