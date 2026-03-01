// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package execution

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var _ orchestration.ExecutionOrchestrator = (*Service)(nil)

// Service handles the execution of tools and manages turn-level orchestration.
type Service struct {
	mu       sync.RWMutex
	registry tools.IToolRegistry
	security security.ISecurityManager
	bus      events.EventBus
}

// Option defines a functional option for initializing the Service.
type Option func(*Service)

// WithRegistry sets the tool registry for the service.
func WithRegistry(r tools.IToolRegistry) Option {
	return func(s *Service) {
		s.registry = r
	}
}

// WithSecurity sets the security manager for the service.
func WithSecurity(sm security.ISecurityManager) Option {
	return func(s *Service) {
		s.security = sm
	}
}

// WithEventBus sets the event bus for the service.
func WithEventBus(bus events.EventBus) Option {
	return func(s *Service) {
		s.bus = bus
	}
}

// NewService creates a new ExecutionOrchestrator service with functional options.
func NewService(opts ...Option) *Service {
	s := &Service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Execute identifies tool calls in the content, validates them, executes them,
// and returns a new Content containing the results.
func (s *Service) Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("tool registry not initialized")
	}

	calls := s.extractFunctionCalls(content)
	if len(calls) == 0 {
		return nil, nil
	}

	if turn >= maxTurns {
		s.emit(ctx, events.SystemMessageEvent{
			Message: fmt.Sprintf("Maximum tool execution turns (%d) reached.", maxTurns),
			Level:   "error",
		})
		return nil, llm.ErrMaxTurnsReached
	}

	s.emit(ctx, events.ToolCallEvent{
		Calls:    calls,
		Turn:     turn,
		MaxTurns: maxTurns,
	})

	var responseParts []*llm.Part
	for _, call := range calls {
		// 1. Security Check
		if s.security != nil {
			// Basic security check: if it's a command, check if allowed.
			// This is a simplified version of the full security check.
			if cmd, ok := call.Args["command"].(string); ok {
				if !s.security.IsCommandAllowed(cmd) {
					responseParts = append(responseParts, s.formatResult(call, tools.ToolResult{
						Text: "Security Block: This command is not allowed.",
					}))
					continue
				}
			}
		}

		// 2. Execution
		result, err := s.registry.Execute(ctx, call.Name, call.Args)
		if err != nil {
			responseParts = append(responseParts, s.formatResult(call, tools.ToolResult{
				Text:  fmt.Sprintf("Error: %v", err),
				Error: err,
			}))
			continue
		}

		// 3. Collect Result
		responseParts = append(responseParts, s.formatResult(call, result))
		
		s.emit(ctx, events.ToolResultEvent{
			Name:   call.Name,
			Result: result,
		})
	}

	return &llm.Content{
		Role:  "user",
		Parts: responseParts,
	}, nil
}

func (s *Service) extractFunctionCalls(content *llm.Content) []*llm.FunctionCall {
	var calls []*llm.FunctionCall
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	return calls
}

func (s *Service) formatResult(call *llm.FunctionCall, result tools.ToolResult) *llm.Part {
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			ID:       call.ID,
			Name:     call.Name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

func (s *Service) emit(ctx context.Context, e events.Event) {
	if s.bus != nil {
		_ = s.bus.Publish(ctx, e)
	}
}
