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
	// ErrQuotaExhausted signals that the provider's quota or account balance
	// has been exhausted. Unlike ErrRateLimit (transient backpressure),
	// quota exhaustion is terminal for THIS provider — do not retry
	// same-provider — but it must NOT abort cross-provider failover chains.
	// The failover gateway skips an exhausted provider and tries the next one.
	ErrQuotaExhausted = errors.New("quota exhausted")
	// ErrContentFilter signals that the provider rejected the request
	// due to content safety policy. The error message contains the
	// provider's reason, which is surfaced to the user/operator so they
	// can adjust the prompt or input before retrying.
	ErrContentFilter = errors.New("content filter rejection")
)

// IsTransient returns true if the error is ErrTransient or ErrRateLimit.
func IsTransient(err error) bool {
	return errors.Is(err, ErrTransient) || errors.Is(err, ErrRateLimit)
}

// IsTerminal returns true if the error is ErrTerminal or other non-retryable domain errors.
func IsTerminal(err error) bool {
	return errors.Is(err, ErrTerminal) ||
		errors.Is(err, errBudgetExceeded) ||
		errors.Is(err, ErrMaxTurnsReached) ||
		errors.Is(err, ErrContextLimitExceeded) ||
		errors.Is(err, ErrContentFilter)
}

// IsAuth returns true if the error is ErrAuth.
func IsAuth(err error) bool {
	return errors.Is(err, ErrAuth)
}

// LLMGateway defines the interface for resilient AI model interactions.
type LLMGateway interface {
	// Generate handles auth retries and synchronous chat response.
	Generate(ctx context.Context, input []*Content, tools []*tools.ToolDeclaration, resolver AssetResolver) (*Content, *Metrics, error)
}
