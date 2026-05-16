// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// seedContents creates n Content entries with distinct role/text for testing.
func seedContents(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		contents[i] = &llm.Content{
			Role: "user",
			Parts: []*llm.Part{
				{Text: string(rune('a' + i))},
			},
		}
	}
	return contents
}

func TestMockHistoryManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockHistoryManager
		check func(t *testing.T, m *MockHistoryManager)
	}{
		{
			name: "AddContent_appends",
			setup: func() *MockHistoryManager {
				return &MockHistoryManager{}
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				content := &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}
				err := m.AddContent(context.Background(), content)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if m.GetTotalEntries() != 1 {
					t.Fatalf("got %d entries; want 1", m.GetTotalEntries())
				}
				contents := m.GetContents()
				if len(contents) != 1 || contents[0].Role != "user" || contents[0].Parts[0].Text != "hello" {
					t.Errorf("got %+v; want role=user, text=hello", contents)
				}
			},
		},
		{
			name: "GetWindow_normal_range",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(3))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				window, err := m.GetWindow(context.Background(), 0, 2)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(window) != 2 {
					t.Fatalf("got %d items; want 2", len(window))
				}
				// Verify deep copy: mutating window should not affect internal contents.
				window[0].Parts[0].Text = "mutated"
				internal := m.GetContents()
				if internal[0].Parts[0].Text == "mutated" {
					t.Error("GetWindow did not deep-copy; internal state was mutated")
				}
			},
		},
		{
			name: "GetWindow_endIdx_negative_one",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(3))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				window, err := m.GetWindow(context.Background(), 1, -1)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(window) != 2 {
					t.Errorf("got %d items; want 2 (indices 1..end)", len(window))
				}
			},
		},
		{
			name: "GetWindow_out_of_bounds_start",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(3))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				window, err := m.GetWindow(context.Background(), 10, 20)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(window) != 0 {
					t.Errorf("got %d items; want 0 (start clamped to total)", len(window))
				}
			},
		},
		{
			name: "GetWindow_reversed_range",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(3))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				window, err := m.GetWindow(context.Background(), 2, 1)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(window) != 0 {
					t.Errorf("got %d items; want 0 (reversed range)", len(window))
				}
			},
		},
		{
			name: "GetWindow_with_error",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(3))
				m.SetGetWindowErr(errors.New("fail"))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				window, err := m.GetWindow(context.Background(), 0, -1)
				if err == nil || err.Error() != "fail" {
					t.Errorf("got error %v; want 'fail'", err)
				}
				if window != nil {
					t.Errorf("got %v; want nil", window)
				}
			},
		},
		{
			name: "RollbackTurns_normal",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(6))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				removed, remaining, msgs, err := m.RollbackTurns(context.Background(), 1)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if removed != 1 {
					t.Errorf("got removed=%d; want 1", removed)
				}
				if remaining != 2 {
					t.Errorf("got remaining=%d; want 2", remaining)
				}
				if msgs != 4 {
					t.Errorf("got msgs=%d; want 4", msgs)
				}
			},
		},
		{
			name: "RollbackTurns_removes_all",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(4))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				removed, remaining, msgs, err := m.RollbackTurns(context.Background(), 5)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if removed != 2 {
					t.Errorf("got removed=%d; want 2", removed)
				}
				if remaining != 0 {
					t.Errorf("got remaining=%d; want 0", remaining)
				}
				if msgs != 0 {
					t.Errorf("got msgs=%d; want 0", msgs)
				}
				if m.GetTotalEntries() != 0 {
					t.Error("expected contents to be cleared")
				}
			},
		},
		{
			name: "RollbackTurns_zero_turns",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(4))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				removed, remaining, msgs, err := m.RollbackTurns(context.Background(), 0)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if removed != 0 {
					t.Errorf("got removed=%d; want 0", removed)
				}
				if remaining != 2 {
					t.Errorf("got remaining=%d; want 2", remaining)
				}
				if msgs != 4 {
					t.Errorf("got msgs=%d; want 4", msgs)
				}
			},
		},
		{
			name: "RollbackTurns_with_error",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				_ = m.SetContents(context.Background(), seedContents(4))
				m.SetRollbackErr(errors.New("fail"))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				_, _, _, err := m.RollbackTurns(context.Background(), 1)
				if err == nil || err.Error() != "fail" {
					t.Errorf("got error %v; want 'fail'", err)
				}
			},
		},
		{
			name: "SetContents_with_func_override",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				m.SetContentsFunc = func(ctx context.Context, contents []*llm.Content) error {
					// Custom behavior: store only the first content.
					m.Mu.Lock()
					if len(contents) > 0 {
						m.Contents = []*llm.Content{llm.CloneContent(contents[0])}
					} else {
						m.Contents = nil
					}
					m.Mu.Unlock()
					return nil
				}
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				contents := seedContents(3)
				err := m.SetContents(context.Background(), contents)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Func override stores only the first.
				if m.GetTotalEntries() != 1 {
					t.Errorf("got %d entries; want 1 (func override)", m.GetTotalEntries())
				}
			},
		},
		{
			name: "SetContents_with_error",
			setup: func() *MockHistoryManager {
				m := &MockHistoryManager{}
				m.SetSetContentsErr(errors.New("fail"))
				return m
			},
			check: func(t *testing.T, m *MockHistoryManager) {
				err := m.SetContents(context.Background(), seedContents(2))
				if err == nil || err.Error() != "fail" {
					t.Errorf("got error %v; want 'fail'", err)
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
