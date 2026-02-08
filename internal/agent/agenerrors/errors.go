// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenerrors

import (
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Category definitions
var (
	ErrTransient = errors.New("transient failure") // Should retry
	ErrFatal     = errors.New("terminal failure")  // Should stop
	ErrLogic     = errors.New("logic violation")   // Should stop, indicates bug or limit
)

// AgentError provides structured error context for the orchestration engine.
type AgentError struct {
	Category error
	Message  string
	Err      error
}

func (e *AgentError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *AgentError) Unwrap() error {
	return e.Err
}

func (e *AgentError) Is(target error) bool {
	return e.Category == target || errors.Is(e.Category, target)
}

// NewAgentError is a helper for creating categorized errors.
func NewAgentError(category error, message string, err error) error {
	return &AgentError{
		Category: category,
		Message:  message,
		Err:      err,
	}
}

// IsTransient checks if the error should trigger a retry.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	// Domain-level transient errors (e.g., rate limits) or direct category match
	return llm.IsTransient(err) || errors.Is(err, ErrTransient)
}

// IsFatal checks if the error should halt the current turn and session.
func IsFatal(err error) bool {
	if err == nil {
		return false
	}
	// Domain-level fatal errors, budget/limit errors, or direct category match
	return llm.IsTerminal(err) || llm.IsAuth(err) ||
		errors.Is(err, llm.ErrBudgetExceeded) ||
		errors.Is(err, llm.ErrMaxTurnsReached) ||
		errors.Is(err, llm.ErrContextLimitExceeded) ||
		errors.Is(err, ErrFatal) ||
		errors.Is(err, ErrLogic)
}
