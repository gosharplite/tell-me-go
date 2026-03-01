// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package execution

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Service handles tool execution by wrapping the robust ToolExecutor.
type Service struct {
	mu       sync.RWMutex
	executor *executor.ToolExecutor
}

// NewService creates a new execution Service wrapping a ToolExecutor.
func NewService(ex *executor.ToolExecutor) *Service {
	return &Service{executor: ex}
}

// Execute identifies and executes tool calls using the wrapped ToolExecutor.
func (s *Service) Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
	if s.executor == nil {
		return nil, fmt.Errorf("executor not initialized")
	}
	return s.executor.Execute(ctx, content, turn, maxTurns)
}

// Shutdown shuts down the wrapped executor.
func (s *Service) Shutdown() {
	if s.executor != nil {
		s.executor.Shutdown()
	}
}
