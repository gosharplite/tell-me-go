// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"errors"
	"testing"
)

// --- Confirm (3 subtests) ---

func TestMockInteractor_Confirm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockInteractor
		wantOk  bool
		wantErr string
	}{
		{
			name:   "yes",
			mock:   &MockInteractor{Answer: "y"},
			wantOk: true,
		},
		{
			name:   "no",
			mock:   &MockInteractor{Answer: "n"},
			wantOk: false,
		},
		{
			name:    "with_error",
			mock:    &MockInteractor{Err: errors.New("fail")},
			wantOk:  false,
			wantErr: "fail",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok, err := tt.mock.Confirm(context.Background(), "msg")

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("got error %v; want %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if ok != tt.wantOk {
				t.Errorf("got %v; want %v", ok, tt.wantOk)
			}
		})
	}
}

// --- ReadSingleKey (1 subtest) ---

func TestMockInteractor_ReadSingleKey(t *testing.T) {
	t.Parallel()

	mock := &MockInteractor{Answer: "a"}
	key, err := mock.ReadSingleKey(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "a" {
		t.Errorf("got %q; want 'a'", key)
	}
}

// --- Warn (1 subtest) ---

func TestMockInteractor_Warn(t *testing.T) {
	t.Parallel()

	mock := &MockInteractor{}
	mock.Warn("a")
	mock.Warn("b")
	if len(mock.Warns) != 2 || mock.Warns[0] != "a" || mock.Warns[1] != "b" {
		t.Errorf("got %v; want [a b]", mock.Warns)
	}
}

// --- Prompt (1 subtest) ---

func TestMockInteractor_Prompt(t *testing.T) {
	t.Parallel()

	mock := &MockInteractor{}
	mock.Prompt("a")
	if len(mock.Prompts) != 1 || mock.Prompts[0] != "a" {
		t.Errorf("got %v; want [a]", mock.Prompts)
	}
}
