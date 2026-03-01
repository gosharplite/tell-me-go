// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package execution

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
)

var _ orchestration.ExecutionOrchestrator = (*service)(nil)

// service handles tool execution by wrapping the robust ToolExecutor.
type service struct {
	mu       sync.RWMutex
	executor *executor.ToolExecutor
}

// NewService creates a new ExecutionOrchestrator wrapping a ToolExecutor.
func NewService(ex *executor.ToolExecutor) orchestration.ExecutionOrchestrator {
	return &service{executor: ex}
}

// Execute identifies and executes tool calls using the wrapped ToolExecutor.
func (s *service) Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
	if s.executor == nil {
		return nil, fmt.Errorf("executor not initialized")
	}
	return s.executor.Execute(ctx, content, turn, maxTurns)
}

// Shutdown shuts down the wrapped executor.
func (s *service) Shutdown() {
	if s.executor != nil {
		s.executor.Shutdown()
	}
}
