// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenerrors

import (
	"errors"
)

// Category definitions
var (
	ErrTransient = errors.New("transient failure") // Should retry
	ErrFatal     = errors.New("terminal failure")  // Should stop
	ErrLogic     = errors.New("logic violation")   // Should stop, indicates bug or limit

	// Tool-specific error categories
	ErrToolNotFound      = errors.New("tool not found")
	ErrToolTimeout       = errors.New("tool execution timeout")
	ErrSecurityViolation = errors.New("security policy violation")
	ErrInvalidArgs       = errors.New("invalid tool arguments")
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
