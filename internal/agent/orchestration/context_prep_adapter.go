// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_orchestration "github.com/gosharplite/tell-me-go/internal/domain/orchestration"
)

// ContextPrepAdapter satisfies the domain's ContextPreparationService by wrapping the Agent-layer ContextManager.
type ContextPrepAdapter struct {
	cm *ContextManager
}

// NewContextPrepAdapter creates a new ContextPreparationService adapter.
func NewContextPrepAdapter(cm *ContextManager) domain_orchestration.ContextPreparationService {
	return &ContextPrepAdapter{cm: cm}
}

// Prepare delegates to the wrapped ContextManager and drops the metadata return to satisfy the domain interface.
func (a *ContextPrepAdapter) Prepare(ctx context.Context, turn int) ([]*llm.Content, error) {
	history, _, err := a.cm.Prepare(ctx, turn)
	return history, err
}

// AddContent appends new content to the current session history by delegating to the ContextManager.
func (a *ContextPrepAdapter) AddContent(ctx context.Context, content *llm.Content) error {
	return a.cm.AddContent(ctx, content)
}
