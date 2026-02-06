// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Error categories
var (
	ErrTransient = errors.New("transient error")
	ErrFatal     = errors.New("fatal error")
	ErrLogic     = errors.New("logic error")
)

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

func IsTransient(err error) bool {
	if errors.Is(err, llm.ErrTransient) {
		return true
	}
	var ae *AgentError
	if errors.As(err, &ae) {
		return errors.Is(ae.Category, ErrTransient)
	}
	return false
}

func IsFatal(err error) bool {
	if errors.Is(err, llm.ErrTerminal) || errors.Is(err, llm.ErrAuth) {
		return true
	}
	var ae *AgentError
	if errors.As(err, &ae) {
		return errors.Is(ae.Category, ErrFatal)
	}
	return false
}
