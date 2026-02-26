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
	// ErrRateLimit signals a rate limit failure.
	ErrRateLimit = errors.New("rate limit exceeded")
)

// IsTransient returns true if the error is ErrTransient.
func IsTransient(err error) bool {
	return errors.Is(err, ErrTransient)
}

// IsTerminal returns true if the error is ErrTerminal or other non-retryable domain errors.
func IsTerminal(err error) bool {
	return errors.Is(err, ErrTerminal) ||
		errors.Is(err, ErrBudgetExceeded) ||
		errors.Is(err, ErrMaxTurnsReached) ||
		errors.Is(err, ErrContextLimitExceeded)
}

// IsAuth returns true if the error is ErrAuth.
func IsAuth(err error) bool {
	return errors.Is(err, ErrAuth)
}

// LLMGateway defines the interface for resilient AI model interactions.
type LLMGateway interface {
	// Generate handles auth retries and returns a content stream and a finalizer.
	Generate(ctx context.Context, input []*Content, tools []*tools.ToolDeclaration, resolver AssetResolver) (<-chan *Content, func() (*Content, *Metrics, error))
}
