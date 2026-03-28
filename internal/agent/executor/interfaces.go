// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ToolExecutor is the unified interface for both the base runtime and all future decorators.
type ToolExecutor interface {
	Execute(ctx context.Context, call *llm.FunctionCall) (tools.ToolResult, error)
}

// ToolResolutionService abstracts the tool lookup logic.
type ToolResolutionService interface {
	Resolve(call *llm.FunctionCall) (*tools.ToolDeclaration, error)
}
