// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestLLMError_String(t *testing.T) {
	tests := []struct {
		e    LLMError
		want string
	}{
		{LLMErrorRateLimited, "rate_limited"},
		{LLMErrorContextOverflow, "context_overflow"},
		{LLMErrorAuthFailure, "auth_failure"},
		{LLMErrorServerError, "server_error"},
		{LLMErrorTimeout, "timeout"},
		{LLMError(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.e.String(); got != tt.want {
			t.Errorf("LLMError(%d).String() = %q, want %q", tt.e, got, tt.want)
		}
	}
}

func TestClassifyLLMError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want LLMError
	}{
		{"nil error", nil, -1},
		{"rate limit", ErrRateLimit, LLMErrorRateLimited},
		{"auth failure", ErrAuth, LLMErrorAuthFailure},
		{"content filter", ErrContentFilter, LLMErrorAuthFailure},
		{"context overflow", ErrContextLimitExceeded, LLMErrorContextOverflow},
		{"budget exceeded", errBudgetExceeded, LLMErrorContextOverflow},
		{"transient", ErrTransient, LLMErrorServerError},
		{"context deadline", context.DeadlineExceeded, LLMErrorTimeout},
		{"wrapped rate limit", wrapErr("wrapped", ErrRateLimit), LLMErrorRateLimited},
		{"wrapped auth", wrapErr("wrapped", ErrAuth), LLMErrorAuthFailure},
		{"unknown error defaults to server_error", errors.New("unknown"), LLMErrorServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyLLMError(tt.err)
			if got != tt.want {
				t.Errorf("ClassifyLLMError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyLLMError_NetTimeout(t *testing.T) {
	err := &net.OpError{Err: &netTimeoutError{}}
	got := ClassifyLLMError(err)
	if got != LLMErrorTimeout {
		t.Errorf("ClassifyLLMError(net timeout) = %v, want LLMErrorTimeout", got)
	}
}

type netTimeoutError struct{}

func (e *netTimeoutError) Error() string   { return "timeout" }
func (e *netTimeoutError) Timeout() bool   { return true }
func (e *netTimeoutError) Temporary() bool { return true }

// wrapErr returns an error wrapping target with the given message.
func wrapErr(msg string, target error) error {
	return &wrapError{msg: msg, target: target}
}

type wrapError struct {
	msg    string
	target error
}

func (e *wrapError) Error() string { return e.msg + ": " + e.target.Error() }
func (e *wrapError) Unwrap() error { return e.target }
