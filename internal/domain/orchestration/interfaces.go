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
