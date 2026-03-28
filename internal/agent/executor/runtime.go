// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// BaseRuntime implements the raw execution logic of tools.
type BaseRuntime struct {
	registry tools.Registry
}

// NewBaseRuntime creates a new BaseRuntime.
func NewBaseRuntime(registry tools.Registry) *BaseRuntime {
	return &BaseRuntime{
		registry: registry,
	}
}

// Execute performs the raw execution of a tool call.
func (b *BaseRuntime) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (tools.ToolResult, error) {
	// 2. Unmarshal/validate arguments (already done by the LLM in call.Args)
	// 3. Execute the underlying tool logic
	// 4. Return the ToolResult
	return b.registry.Execute(ctx, tool.Name, call.Args)
}
