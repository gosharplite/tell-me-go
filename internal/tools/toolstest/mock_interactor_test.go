// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestMockInteractor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockInteractor
		check func(t *testing.T, m *MockInteractor)
	}{
		{
			name: "Confirm_yes",
			setup: func() *MockInteractor {
				return &MockInteractor{Answer: "y"}
			},
			check: func(t *testing.T, m *MockInteractor) {
				ok, err := m.Confirm(context.Background(), "msg")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !ok {
					t.Error("expected true for answer 'y'")
				}
			},
		},
		{
			name: "Confirm_no",
			setup: func() *MockInteractor {
				return &MockInteractor{Answer: "n"}
			},
			check: func(t *testing.T, m *MockInteractor) {
				ok, err := m.Confirm(context.Background(), "msg")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ok {
					t.Error("expected false for answer 'n'")
				}
			},
		},
		{
			name: "Confirm_with_error",
			setup: func() *MockInteractor {
				return &MockInteractor{Err: errors.New("fail")}
			},
			check: func(t *testing.T, m *MockInteractor) {
				ok, err := m.Confirm(context.Background(), "msg")
				if err == nil || err.Error() != "fail" {
					t.Errorf("got error %v; want 'fail'", err)
				}
				if ok {
					t.Error("expected false when Err is set")
				}
			},
		},
		{
			name: "ReadLine_with_answer",
			setup: func() *MockInteractor {
				return &MockInteractor{Answer: "input"}
			},
			check: func(t *testing.T, m *MockInteractor) {
				line, err := m.ReadLine(context.Background())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if line != "input" {
					t.Errorf("got %q; want 'input'", line)
				}
			},
		},
		{
			name: "ReadLine_empty_returns_EOF",
			setup: func() *MockInteractor {
				return &MockInteractor{Answer: ""}
			},
			check: func(t *testing.T, m *MockInteractor) {
				line, err := m.ReadLine(context.Background())
				if err != io.EOF {
					t.Errorf("got error %v; want io.EOF", err)
				}
				if line != "" {
					t.Errorf("got %q; want ''", line)
				}
			},
		},
		{
			name: "ReadLine_with_error",
			setup: func() *MockInteractor {
				return &MockInteractor{Err: errors.New("fail")}
			},
			check: func(t *testing.T, m *MockInteractor) {
				line, err := m.ReadLine(context.Background())
				if err == nil || err.Error() != "fail" {
					t.Errorf("got error %v; want 'fail'", err)
				}
				if line != "" {
					t.Errorf("got %q; want ''", line)
				}
			},
		},
		{
			name: "ReadSingleKey",
			setup: func() *MockInteractor {
				return &MockInteractor{Answer: "a"}
			},
			check: func(t *testing.T, m *MockInteractor) {
				key, err := m.ReadSingleKey(context.Background())
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if key != "a" {
					t.Errorf("got %q; want 'a'", key)
				}
			},
		},
		{
			name: "Warn_appends",
			setup: func() *MockInteractor {
				return &MockInteractor{}
			},
			check: func(t *testing.T, m *MockInteractor) {
				m.Warn("a")
				m.Warn("b")
				if len(m.Warns) != 2 || m.Warns[0] != "a" || m.Warns[1] != "b" {
					t.Errorf("got %v; want [a b]", m.Warns)
				}
			},
		},
		{
			name: "Prompt_appends",
			setup: func() *MockInteractor {
				return &MockInteractor{}
			},
			check: func(t *testing.T, m *MockInteractor) {
				m.Prompt("a")
				if len(m.Prompts) != 1 || m.Prompts[0] != "a" {
					t.Errorf("got %v; want [a]", m.Prompts)
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
