// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Category definitions
var (
	// ErrLogic is strictly for agent loop/Turn limits.
	// Note: tool-level human rejections are handled via domaintools.ErrUserDeclined sentinels,
	// which are NOT categorized as ErrLogic.
	ErrLogic = errors.New("logic violation") // Should stop, indicates bug or limit
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

// isTransient checks if the error should trigger a retry.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	return llm.IsTransient(err)
}

// isFatal checks if the error should halt the current Turn and session.
func isFatal(err error) bool {
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
