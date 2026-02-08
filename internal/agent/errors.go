// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"github.com/gosharplite/tell-me-go/internal/agent/agenerrors"
)

// Re-export types and variables for backward compatibility
type AgentError = agenerrors.AgentError

var (
	ErrTransient = agenerrors.ErrTransient
	ErrFatal     = agenerrors.ErrFatal
	ErrLogic     = agenerrors.ErrLogic
)

// IsTransient checks if the error should trigger a retry.
func IsTransient(err error) bool {
	return agenerrors.IsTransient(err)
}

// IsFatal checks if the error should halt the current turn and session.
func IsFatal(err error) bool {
	return agenerrors.IsFatal(err)
}

// NewAgentError is a helper for creating categorized errors.
func NewAgentError(category error, message string, err error) error {
	return agenerrors.NewAgentError(category, message, err)
}
