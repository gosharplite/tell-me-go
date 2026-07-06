// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
)

// LLMError classifies a provider error into one of the five categories
// defined by the domain model. The orchestrator switches on this type
// to decide retry, failover, or abort behavior.
type LLMError int

const (
	// LLMErrorRateLimited — the provider's quota has been exceeded; retry after backoff.
	LLMErrorRateLimited LLMError = iota
	// LLMErrorContextOverflow — the accumulated prompt exceeds the model's context window.
	LLMErrorContextOverflow
	// LLMErrorAuthFailure — the API key or credentials are invalid; do not retry.
	LLMErrorAuthFailure
	// LLMErrorServerError — a transient 5xx from the provider; may succeed on retry or failover.
	LLMErrorServerError
	// LLMErrorTimeout — the provider did not respond within the configured timeout.
	LLMErrorTimeout
)

// String returns the domain model's kebab-case identifier for this error category.
func (e LLMError) String() string {
	switch e {
	case LLMErrorRateLimited:
		return "rate_limited"
	case LLMErrorContextOverflow:
		return "context_overflow"
	case LLMErrorAuthFailure:
		return "auth_failure"
	case LLMErrorServerError:
		return "server_error"
	case LLMErrorTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// ClassifyLLMError maps an error chain to its LLMError category.
// It walks the error chain via errors.Is, checking the most specific
// sentinels first (rate limit, auth, context overflow) before falling
// back to broader categories (timeout → server_error → unknown).
//
// Returns -1 if err is nil (callers should check this before switching).
func ClassifyLLMError(err error) LLMError {
	if err == nil {
		return -1
	}

	// Specific sentinels first
	if errors.Is(err, ErrRateLimit) {
		return LLMErrorRateLimited
	}
	if errors.Is(err, ErrAuth) {
		return LLMErrorAuthFailure
	}
	if errors.Is(err, ErrContextLimitExceeded) || errors.Is(err, errBudgetExceeded) {
		return LLMErrorContextOverflow
	}

	// Timeout: context deadline or net.Error timeout
	if errors.Is(err, context.DeadlineExceeded) {
		return LLMErrorTimeout
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return LLMErrorTimeout
	}

	// Transient (server errors, 5xx, gRPC Unavailable, etc.)
	if errors.Is(err, ErrTransient) {
		return LLMErrorServerError
	}

	// Default: classify as server error (retryable by default)
	return LLMErrorServerError
}
