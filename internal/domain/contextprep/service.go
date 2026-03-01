// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package contextprep

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

var _ orchestration.ContextPreparationService = (*Service)(nil)

// Service provides context preparation logic for LLM orchestration.
type Service struct {
	mu      sync.RWMutex
	history ports.HistoryManager
	loader  config.ConfigLoader
	bus     events.EventBus
}

// Option defines a functional option for initializing the Service.
type Option func(*Service)

// WithHistory sets the history manager for the service.
func WithHistory(h ports.HistoryManager) Option {
	return func(s *Service) {
		s.history = h
	}
}

// WithLoader sets the config loader for the service.
func WithLoader(l config.ConfigLoader) Option {
	return func(s *Service) {
		s.loader = l
	}
}

// WithEventBus sets the event bus for the service.
func WithEventBus(bus events.EventBus) Option {
	return func(s *Service) {
		s.bus = bus
	}
}

// NewService creates a new ContextPreparationService with functional options.
func NewService(opts ...Option) *Service {
	s := &Service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Prepare gathers history and prepares it for the LLM turn.
func (s *Service) Prepare(ctx context.Context, turn int) ([]*llm.Content, error) {
	if s.history == nil {
		return nil, fmt.Errorf("history manager not initialized")
	}

	// For now, we perform a simple window fetch.
	// Future iterations will include the more complex pipeline logic (summarization, pruning).
	history, err := s.history.GetWindow(ctx, 0, -1)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}

	return history, nil
}

// AddContent appends new content to the session history.
func (s *Service) AddContent(ctx context.Context, content *llm.Content) error {
	if s.history == nil {
		return fmt.Errorf("history manager not initialized")
	}

	// Validate role alternation (simple check for now)
	total := s.history.GetTotalEntries()
	if total > 0 {
		lastIdx := total - 1
		lastWindow, err := s.history.GetWindow(ctx, lastIdx, -1)
		if err != nil {
			return err
		}
		if len(lastWindow) > 0 {
			last := lastWindow[0]
			if last.Role == content.Role {
				// Append parts to the last entry if same role
				return s.history.AppendParts(ctx, lastIdx, content.Parts)
			}
		}
	} else if content.Role != "user" {
		return fmt.Errorf("first message must be 'user', got '%s'", content.Role)
	}

	return s.history.AddContent(ctx, content)
}
