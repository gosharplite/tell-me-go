// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmcoord

import (
	"context"
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var _ orchestration.LLMCoordinator = (*service)(nil)

var (
	// ErrGatewayNotInitialized signals that the service was created without a gateway.
	ErrGatewayNotInitialized = errors.New("llm gateway not initialized")
	// ErrNilContentReturned signals that the gateway returned an empty or nil response content.
	ErrNilContentReturned = errors.New("api returned nil content")
)

// service coordinates interactions with the LLM gateway.
type service struct {
	gateway       llm.LLMGateway
	streamHandler func(context.Context, <-chan *llm.Content)
}

// option defines a functional option for initializing the service.
type option func(*service)

// WithGateway sets the LLM gateway for the service.
func WithGateway(g llm.LLMGateway) option {
	return func(s *service) {
		s.gateway = g
	}
}

// WithStreamHandler sets the stream handler for the service.
func WithStreamHandler(handler func(context.Context, <-chan *llm.Content)) option {
	return func(s *service) {
		s.streamHandler = handler
	}
}

// NewService creates a new LLMCoordinator service with functional options.
func NewService(opts ...option) orchestration.LLMCoordinator {
	s := &service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Generate coordinates the LLM generation process.
func (s *service) Generate(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if s.gateway == nil {
		return nil, nil, ErrGatewayNotInitialized
	}

	respCh, finalize := s.gateway.Generate(ctx, history, toolDecls, resolver)

	handler := s.streamHandler

	if handler != nil {
		handler(ctx, respCh)
	} else {
		// Drain the channel if no handler is provided.
		// When ctx is canceled, the gateway is responsible for closing respCh.
		for range respCh {
			// Discard tokens to prevent blocking the sender
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return respContent, metrics, err
	}

	if respContent == nil {
		return nil, nil, ErrNilContentReturned
	}

	return respContent, metrics, nil
}
