// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package contextprep

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_orchestration "github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

var _ domain_orchestration.ContextPreparationService = (*service)(nil)

// service provides context preparation logic by wrapping the robust ContextManager.
type service struct {
	cm *orchestration.ContextManager
}

// NewService creates a new ContextPreparationService wrapping a ContextManager.
func NewService(cm *orchestration.ContextManager) domain_orchestration.ContextPreparationService {
	return &service{cm: cm}
}

// Prepare delegates to the wrapped ContextManager.
func (s *service) Prepare(ctx context.Context, turn int) ([]*llm.Content, error) {
	if s.cm == nil {
		return nil, fmt.Errorf("context manager not initialized")
	}
	history, _, err := s.cm.Prepare(ctx, turn)
	return history, err
}

// AddContent delegates to the wrapped ContextManager.
func (s *service) AddContent(ctx context.Context, content *llm.Content) error {
	if s.cm == nil {
		return fmt.Errorf("context manager not initialized")
	}
	return s.cm.AddContent(ctx, content)
}
