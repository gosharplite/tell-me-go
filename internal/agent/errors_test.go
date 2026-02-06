// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestErrorCategorization(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		isTransient bool
		isFatal     bool
	}{
		{
			name:        "nil error",
			err:         nil,
			isTransient: false,
			isFatal:     false,
		},
		{
			name:        "llm transient error",
			err:         llm.ErrTransient,
			isTransient: true,
			isFatal:     false,
		},
		{
			name:        "llm terminal error",
			err:         llm.ErrTerminal,
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "llm auth error",
			err:         llm.ErrAuth,
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "llm budget exceeded",
			err:         llm.ErrBudgetExceeded,
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "llm max turns reached",
			err:         llm.ErrMaxTurnsReached,
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "llm context limit exceeded",
			err:         llm.ErrContextLimitExceeded,
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "agent transient error",
			err:         NewAgentError(ErrTransient, "retry me", nil),
			isTransient: true,
			isFatal:     false,
		},
		{
			name:        "agent fatal error",
			err:         NewAgentError(ErrFatal, "terminal failure", nil),
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "agent logic error",
			err:         NewAgentError(ErrLogic, "programmer error", nil),
			isTransient: false,
			isFatal:     true,
		},
		{
			name:        "wrapped llm transient error",
			err:         NewAgentError(ErrFatal, "fatal wrap of transient", llm.ErrTransient),
			isTransient: true, // llm.IsTransient(err) check happens first and uses errors.Is which unwraps
			isFatal:     true, // errors.As finds AgentError with Category ErrFatal
		},
		{
			name:        "generic error",
			err:         errors.New("generic error"),
			isTransient: false,
			isFatal:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransient(tt.err); got != tt.isTransient {
				t.Errorf("IsTransient(%v) = %v, want %v", tt.err, got, tt.isTransient)
			}
			if got := IsFatal(tt.err); got != tt.isFatal {
				t.Errorf("IsFatal(%v) = %v, want %v", tt.err, got, tt.isFatal)
			}
		})
	}
}

func TestAgentError_Error(t *testing.T) {
	err := NewAgentError(ErrTransient, "message", errors.New("inner"))
	got := err.Error()
	want := "message: inner"
	if got != want {
		t.Errorf("AgentError.Error() = %q, want %q", got, want)
	}

	errNoInner := NewAgentError(ErrTransient, "message", nil)
	gotNoInner := errNoInner.Error()
	wantNoInner := "message"
	if gotNoInner != wantNoInner {
		t.Errorf("AgentError.Error() = %q, want %q", gotNoInner, wantNoInner)
	}
}
