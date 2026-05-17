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

// newMockWithContents creates a MockHistoryManager seeded with n content entries.
func newMockWithContents(n int) *MockHistoryManager {
	m := &MockHistoryManager{}
	_ = m.SetContents(context.Background(), seedContents(n))
	return m
}

// newMockWithContentsAndWindowErr creates a seeded mock that returns err from GetWindow.
func newMockWithContentsAndWindowErr(n int, err error) *MockHistoryManager {
	m := newMockWithContents(n)
	m.SetGetWindowErr(err)
	return m
}

// newMockWithContentsAndRollbackErr creates a seeded mock that returns err from RollbackTurns.
func newMockWithContentsAndRollbackErr(n int, err error) *MockHistoryManager {
	m := newMockWithContents(n)
	m.SetRollbackErr(err)
	return m
}

// ---------------------------------------------------------------------------
// AddContent
// ---------------------------------------------------------------------------

func TestMockHistoryManager_AddContent_Appends(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
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
}

// ---------------------------------------------------------------------------
// GetWindow
// ---------------------------------------------------------------------------

func TestMockHistoryManager_GetWindow_NormalRange(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(3)

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
}

func TestMockHistoryManager_GetWindow_EndIdxNegativeOne(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(3)

	window, err := m.GetWindow(context.Background(), 1, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(window) != 2 {
		t.Errorf("got %d items; want 2 (indices 1..end)", len(window))
	}
}

func TestMockHistoryManager_GetWindow_OutOfBoundsStart(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(3)

	window, err := m.GetWindow(context.Background(), 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(window) != 0 {
		t.Errorf("got %d items; want 0 (start clamped to total)", len(window))
	}
}

func TestMockHistoryManager_GetWindow_ReversedRange(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(3)

	window, err := m.GetWindow(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(window) != 0 {
		t.Errorf("got %d items; want 0 (reversed range)", len(window))
	}
}

func TestMockHistoryManager_GetWindow_WithError(t *testing.T) {
	t.Parallel()

	m := newMockWithContentsAndWindowErr(3, errors.New("fail"))

	window, err := m.GetWindow(context.Background(), 0, -1)
	if err == nil || err.Error() != "fail" {
		t.Errorf("got error %v; want 'fail'", err)
	}
	if window != nil {
		t.Errorf("got %v; want nil", window)
	}
}

// ---------------------------------------------------------------------------
// RollbackTurns
// ---------------------------------------------------------------------------

func TestMockHistoryManager_RollbackTurns_Normal(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(6)

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
}

func TestMockHistoryManager_RollbackTurns_RemovesAll(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(4)

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
}

func TestMockHistoryManager_RollbackTurns_ZeroTurns(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(4)

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
}

func TestMockHistoryManager_RollbackTurns_WithError(t *testing.T) {
	t.Parallel()

	m := newMockWithContentsAndRollbackErr(4, errors.New("fail"))

	_, _, _, err := m.RollbackTurns(context.Background(), 1)
	if err == nil || err.Error() != "fail" {
		t.Errorf("got error %v; want 'fail'", err)
	}
}

// ---------------------------------------------------------------------------
// SetContents
// ---------------------------------------------------------------------------

func TestMockHistoryManager_SetContents_WithFuncOverride(t *testing.T) {
	t.Parallel()

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

	err := m.SetContents(context.Background(), seedContents(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Func override stores only the first.
	if m.GetTotalEntries() != 1 {
		t.Errorf("got %d entries; want 1 (func override)", m.GetTotalEntries())
	}
}

func TestMockHistoryManager_SetContents_WithError(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	m.SetSetContentsErr(errors.New("fail"))

	err := m.SetContents(context.Background(), seedContents(2))
	if err == nil || err.Error() != "fail" {
		t.Errorf("got error %v; want 'fail'", err)
	}
}
