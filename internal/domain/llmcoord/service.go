// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmcoord

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var _ orchestration.LLMCoordinator = (*service)(nil)

// service coordinates interactions with the LLM gateway.
type service struct {
	gateway       llm.LLMGateway
	streamHandler func(context.Context, <-chan *llm.Content)
}

// Option defines a functional option for initializing the service.
type Option func(*service)

// WithGateway sets the LLM gateway for the service.
func WithGateway(g llm.LLMGateway) Option {
	return func(s *service) {
		s.gateway = g
	}
}

// WithStreamHandler sets the stream handler for the service.
func WithStreamHandler(handler func(context.Context, <-chan *llm.Content)) Option {
	return func(s *service) {
		s.streamHandler = handler
	}
}

// NewService creates a new LLMCoordinator service with functional options.
func NewService(opts ...Option) orchestration.LLMCoordinator {
	s := &service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Generate coordinates the LLM generation process.
func (s *service) Generate(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if s.gateway == nil {
		return nil, nil, fmt.Errorf("llm gateway not initialized")
	}

	respCh, finalize := s.gateway.Generate(ctx, history, toolDecls, resolver)

	handler := s.streamHandler

	if handler != nil {
		handler(ctx, respCh)
	} else {
		// Drain the channel if no handler is provided
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				break drainLoop
			case _, ok := <-respCh:
				if !ok {
					break drainLoop
				}
			}
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return respContent, metrics, err
	}

	if respContent == nil {
		return nil, nil, fmt.Errorf("api returned nil content")
	}

	return respContent, metrics, nil
}
