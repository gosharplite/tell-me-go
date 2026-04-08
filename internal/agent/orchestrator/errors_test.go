// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestIsTransient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"transient error", llm.ErrTransient, true},
		{"rate limit error", llm.ErrRateLimit, true},
		{"terminal error", llm.ErrTerminal, false},
		{"wrapped transient", NewAgentError(llm.ErrTransient, "msg", llm.ErrTransient), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsTransient(tt.err); got != tt.want {
				t.Errorf("IsTransient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFatal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"terminal error", llm.ErrTerminal, true},
		{"auth error", llm.ErrAuth, true},
		{"logic violation", ErrLogic, true},
		{"transient error", llm.ErrTransient, false},
		{"wrapped terminal", NewAgentError(llm.ErrTerminal, "msg", llm.ErrTerminal), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsFatal(tt.err); got != tt.want {
				t.Errorf("IsFatal() = %v, want %v", got, tt.want)
			}
		})
	}
}
