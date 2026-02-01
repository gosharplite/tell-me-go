// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockHttpStatusErr struct {
	code int
}

func (m mockHttpStatusErr) StatusCode() int { return m.code }
func (m mockHttpStatusErr) Error() string  { return fmt.Sprintf("HTTP %d", m.code) }

func TestResilientClient_WrapError(t *testing.T) {
	client := &ResilientClient{}

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{"Nil error", nil, nil},
		{"Already Auth", ErrAuth, ErrAuth},
		{"Already Transient", ErrTransient, ErrTransient},
		{"Already Terminal", ErrTerminal, ErrTerminal},

		{"gRPC Unauthenticated", status.Error(codes.Unauthenticated, "fail"), ErrAuth},
		{"gRPC Unavailable", status.Error(codes.Unavailable, "fail"), ErrTransient},
		{"gRPC PermissionDenied", status.Error(codes.PermissionDenied, "fail"), ErrTerminal},

		{"HTTP 401", mockHttpStatusErr{401}, ErrAuth},
		{"HTTP 429", mockHttpStatusErr{429}, ErrTransient},
		{"HTTP 500", mockHttpStatusErr{500}, ErrTransient},
		{"HTTP 404", mockHttpStatusErr{404}, ErrTerminal},

		{"String match Auth", errors.New("API_KEY_INVALID"), ErrAuth},
		{"String match Auth Upper", errors.New("unauthenticated request"), ErrAuth},

		{"Generic fallback", errors.New("unknown error"), ErrTerminal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.WrapError(tt.err)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("WrapError() = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tt.expected) {
				t.Errorf("WrapError() = %v, want error containing %v", got, tt.expected)
			}
		})
	}
}
