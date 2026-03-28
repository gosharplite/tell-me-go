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
	resolver ToolResolutionService
	registry tools.Registry
}

// NewBaseRuntime creates a new BaseRuntime.
func NewBaseRuntime(resolver ToolResolutionService, registry tools.Registry) *BaseRuntime {
	return &BaseRuntime{
		resolver: resolver,
		registry: registry,
	}
}

// Execute performs the raw execution of a tool call.
func (b *BaseRuntime) Execute(ctx context.Context, call *llm.FunctionCall) (tools.ToolResult, error) {
	// 1. Resolve tool using b.resolver
	tool, err := b.resolver.Resolve(call)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 2. Unmarshal/validate arguments (already done by the LLM in call.Args)
	// 3. Execute the underlying tool logic
	// 4. Return the ToolResult
	return b.registry.Execute(ctx, tool.Name, call.Args)
}
