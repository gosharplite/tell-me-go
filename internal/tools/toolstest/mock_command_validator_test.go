// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"errors"
	"strings"
	"testing"
)

func TestMockCommandValidator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockCommandValidator
		check func(t *testing.T, m *MockCommandValidator)
	}{
		{
			name: "Split_default",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				parts, err := m.Split("echo hello")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(parts) != 2 || parts[0] != "echo" || parts[1] != "hello" {
					t.Errorf("got %v; want [echo hello]", parts)
				}
			},
		},
		{
			name: "Split_unclosed_single_quote",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				parts, err := m.Split("echo 'unclosed")
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "unclosed quote") {
					t.Errorf("error %q does not contain 'unclosed quote'", err.Error())
				}
				if parts != nil {
					t.Errorf("expected nil parts, got %v", parts)
				}
			},
		},
		{
			name: "Split_unclosed_double_quote",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				parts, err := m.Split(`echo "unclosed`)
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "unclosed quote") {
					t.Errorf("error %q does not contain 'unclosed quote'", err.Error())
				}
				if parts != nil {
					t.Errorf("expected nil parts, got %v", parts)
				}
			},
		},
		{
			name: "Split_with_func_override",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{
					SplitFunc: func(cmd string) ([]string, error) {
						return []string{"custom", "result"}, errors.New("custom error")
					},
				}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				parts, err := m.Split("anything")
				if err == nil || err.Error() != "custom error" {
					t.Errorf("got error %v; want 'custom error'", err)
				}
				if len(parts) != 2 || parts[0] != "custom" || parts[1] != "result" {
					t.Errorf("got %v; want [custom result]", parts)
				}
			},
		},
		{
			name: "IsSafe_default",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				safe, msg := m.IsSafe("any")
				if !safe {
					t.Error("expected safe=true")
				}
				if msg != "" {
					t.Errorf("expected empty msg, got %q", msg)
				}
			},
		},
		{
			name: "ValidateStructure_default",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				err := m.ValidateStructure([]string{"a"})
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			},
		},
		{
			name: "CheckPathSafety_default",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				safe, msg := m.CheckPathSafety([]string{"a"})
				if !safe {
					t.Error("expected safe=true")
				}
				if msg != "" {
					t.Errorf("expected empty msg, got %q", msg)
				}
			},
		},
		{
			name: "HasShellFeatures_powershell_cmdlet",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				if !m.HasShellFeatures([]string{"Get-ChildItem"}) {
					t.Error("expected true for PowerShell cmdlet")
				}
			},
		},
		{
			name: "HasShellFeatures_variable",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				if !m.HasShellFeatures([]string{"$env:PATH"}) {
					t.Error("expected true for shell variable")
				}
			},
		},
		{
			name: "HasShellFeatures_plain",
			setup: func() *MockCommandValidator {
				return &MockCommandValidator{}
			},
			check: func(t *testing.T, m *MockCommandValidator) {
				if m.HasShellFeatures([]string{"ls", "-la"}) {
					t.Error("expected false for plain command")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := tt.setup()
			tt.check(t, m)
		})
	}
}
