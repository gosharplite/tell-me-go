// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmerr

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func FuzzHTTPStatusToDomain(f *testing.F) {
	// Seed corpus: values from existing table-driven tests, boundary
	// conditions, and high-value edges.
	seeds := []int{
		200, 0, 400, 401, 404, 408, 429, 499, 500, 502, 503,
		-1, 999, // additional boundaries
		399, 402, 498, 599, 600, // high-value edges
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, status int) {
		// P1 (No panic): implicit — any panic fails the test.

		// P2 (Valid sentinel): the return value must be nil or one of
		// the four domain sentinels.
		err := httpStatusToDomain(status)
		if err != nil {
			if !errors.Is(err, llm.ErrAuth) &&
				!errors.Is(err, llm.ErrRateLimit) &&
				!errors.Is(err, llm.ErrTransient) &&
				!errors.Is(err, llm.ErrTerminal) {
				t.Errorf("httpStatusToDomain(%d) = %v; want nil or a domain sentinel", status, err)
			}
		}
	})
}

func TestHTTPStatusToDomain_Seeds(t *testing.T) {
	tests := []struct {
		status   int
		expected error
	}{
		// Below 400 → nil
		{status: -1, expected: nil},
		{status: 0, expected: nil},
		{status: 200, expected: nil},
		{status: 399, expected: nil},

		// 4xx special cases
		{status: 401, expected: llm.ErrAuth},
		{status: 408, expected: llm.ErrTransient},
		{status: 429, expected: llm.ErrRateLimit},
		{status: 499, expected: llm.ErrTransient},

		// Generic 4xx → ErrTerminal
		{status: 400, expected: llm.ErrTerminal},
		{status: 402, expected: llm.ErrTerminal},
		{status: 404, expected: llm.ErrTerminal},
		{status: 498, expected: llm.ErrTerminal},

		// 5xx and above → ErrTransient
		{status: 500, expected: llm.ErrTransient},
		{status: 502, expected: llm.ErrTransient},
		{status: 503, expected: llm.ErrTransient},
		{status: 599, expected: llm.ErrTransient},
		{status: 600, expected: llm.ErrTransient},
		{status: 999, expected: llm.ErrTransient},
	}

	for _, tt := range tests {
		got := httpStatusToDomain(tt.status)
		if !errors.Is(got, tt.expected) {
			t.Errorf("httpStatusToDomain(%d) = %v; want %v", tt.status, got, tt.expected)
		}
	}
}
