// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenerrors

import (
	"errors"
	"testing"
)

func TestAgentError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *AgentError
		expected string
	}{
		{
			name: "With wrapped error",
			err: &AgentError{
				Message: "something went wrong",
				Err:     errors.New("internal error"),
			},
			expected: "something went wrong: internal error",
		},
		{
			name: "Without wrapped error",
			err: &AgentError{
				Message: "something went wrong",
				Err:     nil,
			},
			expected: "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("AgentError.Error() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAgentError_Unwrap(t *testing.T) {
	inner := errors.New("inner")
	err := &AgentError{Err: inner}

	if got := err.Unwrap(); got != inner {
		t.Errorf("AgentError.Unwrap() = %v, want %v", got, inner)
	}

	if got := errors.Unwrap(err); got != inner {
		t.Errorf("errors.Unwrap(AgentError) = %v, want %v", got, inner)
	}
}

func TestAgentError_Is(t *testing.T) {
	customErr := errors.New("custom")
	tests := []struct {
		name   string
		err    *AgentError
		target error
		want   bool
	}{
		{
			name:   "Match direct category",
			err:    &AgentError{Category: ErrTransient},
			target: ErrTransient,
			want:   true,
		},
		{
			name:   "Match wrapped category",
			err:    &AgentError{Category: NewAgentError(ErrTransient, "wrapped", nil)},
			target: ErrTransient,
			want:   true,
		},
		{
			name:   "No match",
			err:    &AgentError{Category: ErrTransient},
			target: ErrFatal,
			want:   false,
		},
		{
			name:   "No match against other error",
			err:    &AgentError{Category: ErrTransient},
			target: customErr,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Is(tt.target); got != tt.want {
				t.Errorf("AgentError.Is() = %v, want %v", got, tt.want)
			}
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Errorf("errors.Is(AgentError) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewAgentError(t *testing.T) {
	category := ErrLogic
	message := "logic error occurred"
	inner := errors.New("low level error")

	err := NewAgentError(category, message, inner)
	ae, ok := err.(*AgentError)
	if !ok {
		t.Fatalf("NewAgentError did not return an *AgentError")
	}

	if ae.Category != category {
		t.Errorf("Expected Category %v, got %v", category, ae.Category)
	}
	if ae.Message != message {
		t.Errorf("Expected Message %v, got %v", message, ae.Message)
	}
	if ae.Err != inner {
		t.Errorf("Expected Err %v, got %v", inner, ae.Err)
	}
}
