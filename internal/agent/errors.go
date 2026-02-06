// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"errors"

	"github.com/gosharplite/tell-me-go/internal/agent/agenerrors"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Re-export types and variables for backward compatibility
type AgentError = agenerrors.AgentError

var (
	ErrTransient = agenerrors.ErrTransient
	ErrFatal     = agenerrors.ErrFatal
	ErrLogic     = agenerrors.ErrLogic

	ErrToolNotFound      = agenerrors.ErrToolNotFound
	ErrToolTimeout       = agenerrors.ErrToolTimeout
	ErrSecurityViolation = agenerrors.ErrSecurityViolation
	ErrInvalidArgs       = agenerrors.ErrInvalidArgs
)

// IsTransient checks if the error should trigger a retry.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	// Domain-level transient errors (e.g., rate limits)
	if llm.IsTransient(err) {
		return true
	}
	var ae *agenerrors.AgentError
	if errors.As(err, &ae) {
		return errors.Is(ae.Category, ErrTransient)
	}
	return false
}

// IsFatal checks if the error should halt the current turn and session.
func IsFatal(err error) bool {
	if err == nil {
		return false
	}
	// Domain-level fatal errors
	if llm.IsTerminal(err) || llm.IsAuth(err) ||
		errors.Is(err, llm.ErrBudgetExceeded) ||
		errors.Is(err, llm.ErrMaxTurnsReached) ||
		errors.Is(err, llm.ErrContextLimitExceeded) {
		return true
	}
	var ae *agenerrors.AgentError
	if errors.As(err, &ae) {
		return errors.Is(ae.Category, ErrFatal) || errors.Is(ae.Category, ErrLogic)
	}
	return false
}

// NewAgentError is a helper for creating categorized errors.
func NewAgentError(category error, message string, err error) error {
	return agenerrors.NewAgentError(category, message, err)
}
