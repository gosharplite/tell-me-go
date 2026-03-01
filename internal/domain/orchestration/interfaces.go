// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ContextPreparationService defines the interface for preparing LLM context.
type ContextPreparationService interface {
	// Prepare gathers and optimizes history for the next LLM turn.
	Prepare(ctx context.Context, turn int) ([]*llm.Content, error)
	// AddContent appends new content to the current session history.
	AddContent(ctx context.Context, content *llm.Content) error
}

// ExecutionOrchestrator defines the interface for executing tools and managing turn state.
type ExecutionOrchestrator interface {
	// Execute takes the model response, identifies tool calls, executes them,
	// and returns the combined tool results as a new Content object.
	Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error)
}
