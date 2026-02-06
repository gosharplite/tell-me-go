// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"

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
	Generate(ctx context.Context, input []*Content, tools []*tools.ToolDeclaration, resolver AssetResolver) (<-chan *Content, func() (*Content, *Metrics, error))
	SetSystemInstructions(instr string)
}
