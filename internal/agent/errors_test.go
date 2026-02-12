// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"standard error", errors.New("some error"), false},
		{"transient agent error", newAgentError(llm.ErrTransient, "msg", nil), true},
		{"direct ErrTransient", llm.ErrTransient, true},
		{"fatal agent error", newAgentError(llm.ErrTerminal, "msg", nil), false},
		{"wrapped transient", fmt.Errorf("wrapped: %w", newAgentError(llm.ErrTransient, "msg", nil)), true},
		{"llm transient error", llm.ErrTransient, true},
		{"llm wrapped transient", fmt.Errorf("wrapped: %w", llm.ErrTransient), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransient(tt.err); got != tt.want {
				t.Errorf("isTransient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFatal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"standard error", errors.New("some error"), false},
		{"fatal agent error", newAgentError(llm.ErrTerminal, "msg", nil), true},
		{"direct ErrTerminal", llm.ErrTerminal, true},
		{"direct ErrLogic", errLogic, true},
		{"logic agent error", newAgentError(errLogic, "msg", nil), true},
		{"transient agent error", newAgentError(llm.ErrTransient, "msg", nil), false},
		{"wrapped fatal", fmt.Errorf("wrapped: %w", newAgentError(llm.ErrTerminal, "msg", nil)), true},
		{"llm terminal error", llm.ErrTerminal, true},
		{"llm budget exceeded", llm.ErrBudgetExceeded, true},
		{"llm max turns", llm.ErrMaxTurnsReached, true},
		{"llm context limit", llm.ErrContextLimitExceeded, true},
		{"llm auth error", llm.ErrAuth, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFatal(tt.err); got != tt.want {
				t.Errorf("isFatal() = %v, want %v", got, tt.want)
			}
		})
	}
}
