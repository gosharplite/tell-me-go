// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Category definitions
var (
	ErrLogic = errors.New("logic violation") // Should stop, indicates bug or limit
)

// agentError provides structured error context for the orchestration engine.
type agentError struct {
	Category error
	Message  string
	Err      error
}

func (e *agentError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *agentError) Unwrap() error {
	return e.Err
}

func (e *agentError) Is(target error) bool {
	return e.Category == target || errors.Is(e.Category, target)
}

// newAgentError is a helper for creating categorized errors.
func newAgentError(category error, message string, err error) error {
	return &agentError{
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
	return llm.IsTransient(err)
}

// IsFatal checks if the error should halt the current turn and session.
func IsFatal(err error) bool {
	if err == nil {
		return false
	}
	// Check domain classification first
	if llm.IsTerminal(err) || llm.IsAuth(err) {
		return true
	}
	// Check agent-specific logic violations
	return errors.Is(err, ErrLogic)
}
