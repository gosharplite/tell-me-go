// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_orchestration "github.com/gosharplite/tell-me-go/internal/domain/orchestration"
)

// executionAdapter satisfies the domain's ExecutionOrchestrator by wrapping the Agent-layer ToolExecutor.
type executionAdapter struct {
	ex *ToolExecutor
}

// NewExecutionAdapter creates a new ExecutionOrchestrator adapter.
func NewExecutionAdapter(ex *ToolExecutor) domain_orchestration.ExecutionOrchestrator {
	return &executionAdapter{ex: ex}
}

// Execute identifies and executes tool calls using the wrapped ToolExecutor.
func (a *executionAdapter) Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
	if a.ex == nil {
		return nil, fmt.Errorf("executor not initialized")
	}
	return a.ex.Execute(ctx, content, turn, maxTurns)
}
