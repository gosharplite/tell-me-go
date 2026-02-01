// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var (
	// ErrTransient signals a retryable failure (e.g., rate limit, timeout).
	ErrTransient = errors.New("transient error")
	// ErrTerminal signals a non-retryable failure (e.g., invalid request).
	ErrTerminal = errors.New("terminal error")
	// ErrAuth signals an authentication failure.
	ErrAuth = errors.New("authentication error")
)

// LLMGateway defines the interface for resilient AI model interactions.
type LLMGateway interface {
	// Generate handles auth retries and returns a content stream and a finalizer.
	Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error))
}
