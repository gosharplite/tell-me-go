// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package contextprep

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Service provides context preparation logic by wrapping the robust ContextManager.
type Service struct {
	mu sync.RWMutex
	cm *orchestration.ContextManager
}

// NewService creates a new Service wrapping a ContextManager.
func NewService(cm *orchestration.ContextManager) *Service {
	return &Service{cm: cm}
}

// Prepare delegates to the wrapped ContextManager.
func (s *Service) Prepare(ctx context.Context, turn int) ([]*llm.Content, error) {
	if s.cm == nil {
		return nil, fmt.Errorf("context manager not initialized")
	}
	history, _, err := s.cm.Prepare(ctx, turn)
	return history, err
}

// AddContent delegates to the wrapped ContextManager.
func (s *Service) AddContent(ctx context.Context, content *llm.Content) error {
	if s.cm == nil {
		return fmt.Errorf("context manager not initialized")
	}
	return s.cm.AddContent(ctx, content)
}

// SummarizeRange delegates to the wrapped ContextManager.
func (s *Service) SummarizeRange(ctx context.Context, numTurns int, focus string) (string, *llm.Metrics, error) {
	if s.cm == nil {
		return "", nil, fmt.Errorf("context manager not initialized")
	}
	return s.cm.SummarizeRange(ctx, numTurns, focus)
}

// Reconfigure delegates to the wrapped ContextManager.
func (s *Service) Reconfigure(limits ports.ContextMetadata) {
	// Note: Existing Reconfigure takes events.Limits. 
	// For now we'll handle reconfiguration through the event bus subscription 
	// already present in ContextManager.
}
