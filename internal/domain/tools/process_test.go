// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import "testing"

// TestExitError_ErrorFormatting pins the domain type's Error() formatting at
// internal/domain/tools/process.go:64 directly, package-locally. The existing
// fake-runner round-trip test (toolstest/fake_process_runner_test.go:52)
// asserts the wording via errors.As only for Code 3 (its :101); it never pins
// the ADR-074 Decision 3 signal-killed convention (-1) nor the zero-value
// Code 0 case — both pinned here via exact string equality.
func TestExitError_ErrorFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
		want string
	}{
		{"exit-code convention", 3, "exit status 3"},
		{"ADR-074 Decision 3 signal-killed convention", -1, "exit status -1"},
		{"zero-value round-trip", 0, "exit status 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (&ExitError{Code: tt.code}).Error(); got != tt.want {
				t.Errorf("(&ExitError{Code: %d}).Error() = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}
