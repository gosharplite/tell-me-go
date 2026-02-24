// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "positive: ErrTransient",
			err:  ErrTransient,
			want: true,
		},
		{
			name: "positive: wrapped ErrTransient",
			err:  fmt.Errorf("wrapped: %w", ErrTransient),
			want: true,
		},
		{
			name: "negative: unrelated standard error",
			err:  errors.New("random network timeout"),
			want: false,
		},
		{
			name: "negative: ErrTerminal",
			err:  ErrTerminal,
			want: false,
		},
		{
			name: "negative: ErrAuth",
			err:  ErrAuth,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransient(tt.err); got != tt.want {
				t.Errorf("IsTransient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "positive: ErrTerminal",
			err:  ErrTerminal,
			want: true,
		},
		{
			name: "positive: wrapped ErrTerminal",
			err:  fmt.Errorf("wrapped: %w", ErrTerminal),
			want: true,
		},
		{
			name: "positive: ErrBudgetExceeded",
			err:  ErrBudgetExceeded,
			want: true,
		},
		{
			name: "positive: ErrMaxTurnsReached",
			err:  ErrMaxTurnsReached,
			want: true,
		},
		{
			name: "positive: ErrContextLimitExceeded",
			err:  ErrContextLimitExceeded,
			want: true,
		},
		{
			name: "negative: unrelated standard error",
			err:  errors.New("random network timeout"),
			want: false,
		},
		{
			name: "negative: ErrTransient",
			err:  ErrTransient,
			want: false,
		},
		{
			name: "negative: ErrAuth",
			err:  ErrAuth,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTerminal(tt.err); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAuth(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "positive: ErrAuth",
			err:  ErrAuth,
			want: true,
		},
		{
			name: "positive: wrapped ErrAuth",
			err:  fmt.Errorf("wrapped: %w", ErrAuth),
			want: true,
		},
		{
			name: "negative: unrelated standard error",
			err:  errors.New("random network timeout"),
			want: false,
		},
		{
			name: "negative: ErrTransient",
			err:  ErrTransient,
			want: false,
		},
		{
			name: "negative: ErrTerminal",
			err:  ErrTerminal,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuth(tt.err); got != tt.want {
				t.Errorf("IsAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}
