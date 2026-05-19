// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"errors"
	"strings"
	"testing"
)

// --- Split (4 subtests) ---

// assertParts compares got parts against want, handling nil expectations.
func assertParts(t *testing.T, parts, want []string) {
	t.Helper()
	if want != nil {
		if len(parts) != len(want) {
			t.Errorf("got len %d; want %d", len(parts), len(want))
			return
		}
		for i := range want {
			if parts[i] != want[i] {
				t.Errorf("got %v; want %v", parts, want)
				break
			}
		}
	} else if parts != nil {
		t.Errorf("expected nil parts, got %v", parts)
	}
}

func TestMockCommandValidator_Split(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockCommandValidator
		cmd     string
		want    []string
		wantErr string
	}{
		{
			name: "default",
			mock: &MockCommandValidator{},
			cmd:  "echo hello",
			want: []string{"echo", "hello"},
		},
		{
			name:    "unclosed_single_quote",
			mock:    &MockCommandValidator{},
			cmd:     "echo 'unclosed",
			wantErr: "unclosed quote",
		},
		{
			name:    "unclosed_double_quote",
			mock:    &MockCommandValidator{},
			cmd:     `echo "unclosed`,
			wantErr: "unclosed quote",
		},
		{
			name: "with_func_override",
			mock: &MockCommandValidator{
				SplitFunc: func(cmd string) ([]string, error) {
					return []string{"custom", "result"}, errors.New("custom error")
				},
			},
			cmd:     "anything",
			want:    []string{"custom", "result"},
			wantErr: "custom error",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parts, err := tt.mock.Split(tt.cmd)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertParts(t, parts, tt.want)
		})
	}
}

// --- IsSafe (1 subtest) ---

func TestMockCommandValidator_IsSafe(t *testing.T) {
	t.Parallel()

	mock := &MockCommandValidator{}
	safe, msg := mock.IsSafe("any")
	if !safe {
		t.Error("expected safe=true")
	}
	if msg != "" {
		t.Errorf("expected empty msg, got %q", msg)
	}
}

// --- ValidateStructure (1 subtest) ---

func TestMockCommandValidator_ValidateStructure(t *testing.T) {
	t.Parallel()

	mock := &MockCommandValidator{}
	err := mock.ValidateStructure([]string{"a"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- CheckPathSafety (1 subtest) ---

func TestMockCommandValidator_CheckPathSafety(t *testing.T) {
	t.Parallel()

	mock := &MockCommandValidator{}
	safe, msg := mock.CheckPathSafety([]string{"a"})
	if !safe {
		t.Error("expected safe=true")
	}
	if msg != "" {
		t.Errorf("expected empty msg, got %q", msg)
	}
}

// --- HasShellFeatures (3 subtests) ---

func TestMockCommandValidator_HasShellFeatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []string
		want  bool
	}{
		{
			name:  "powershell_cmdlet",
			parts: []string{"Get-ChildItem"},
			want:  true,
		},
		{
			name:  "variable",
			parts: []string{"$env:PATH"},
			want:  true,
		},
		{
			name:  "plain",
			parts: []string{"ls", "-la"},
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := &MockCommandValidator{}
			got := mock.HasShellFeatures(tt.parts)
			if got != tt.want {
				t.Errorf("got %v; want %v", got, tt.want)
			}
		})
	}
}
