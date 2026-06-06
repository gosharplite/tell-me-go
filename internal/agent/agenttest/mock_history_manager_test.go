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

// ---------------------------------------------------------------------------
// GetLastUserMessage
// ---------------------------------------------------------------------------

func TestMockHistoryManager_GetLastUserMessage_Defaults(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	text, idx, err := m.GetLastUserMessage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("got text=%q; want \"\"", text)
	}
	if idx != 0 {
		t.Errorf("got idx=%d; want 0", idx)
	}
}

// ---------------------------------------------------------------------------
// Archive
// ---------------------------------------------------------------------------

func TestMockHistoryManager_Archive_ReturnsNil(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	err := m.Archive(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockHistoryManager_Archive_NonNilContents(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	contents := seedContents(2)
	err := m.Archive(context.Background(), contents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AppendParts
// ---------------------------------------------------------------------------

func TestMockHistoryManager_AppendParts_ValidIndex(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(1) // 1 content with 1 part

	extra := []*llm.Part{{Text: "appended"}}
	err := m.AppendParts(context.Background(), 0, extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents := m.GetContents()
	if len(contents) != 1 {
		t.Fatalf("got %d contents; want 1", len(contents))
	}
	if len(contents[0].Parts) != 2 {
		t.Errorf("got %d parts; want 2", len(contents[0].Parts))
	}
	if contents[0].Parts[1].Text != "appended" {
		t.Errorf("got part[1].Text=%q; want \"appended\"", contents[0].Parts[1].Text)
	}
}

func TestMockHistoryManager_AppendParts_NegativeIndex(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(1) // 1 content with 1 part

	extra := []*llm.Part{{Text: "should-not-be-added"}}
	err := m.AppendParts(context.Background(), -1, extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents := m.GetContents()
	if len(contents) != 1 {
		t.Fatalf("got %d contents; want 1", len(contents))
	}
	if len(contents[0].Parts) != 1 {
		t.Errorf("got %d parts; want 1 (negative index -> no-op)", len(contents[0].Parts))
	}
}

func TestMockHistoryManager_AppendParts_OutOfBounds(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(1) // 1 content with 1 part

	extra := []*llm.Part{{Text: "should-not-be-added"}}
	err := m.AppendParts(context.Background(), 5, extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents := m.GetContents()
	if len(contents) != 1 {
		t.Fatalf("got %d contents; want 1", len(contents))
	}
	if len(contents[0].Parts) != 1 {
		t.Errorf("got %d parts; want 1 (out-of-bounds index -> no-op)", len(contents[0].Parts))
	}
}

// ---------------------------------------------------------------------------
// GetResolver
// ---------------------------------------------------------------------------

type stubResolver struct{}

func (stubResolver) Resolve(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func TestMockHistoryManager_GetResolver_RoundTrip(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	res := &stubResolver{}
	m.resolver = res

	got := m.GetResolver()
	if got == nil {
		t.Fatal("expected non-nil resolver")
	}
	if got != res {
		t.Error("GetResolver did not return the seeded resolver")
	}
}

func TestMockHistoryManager_GetResolver_NilByDefault(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	got := m.GetResolver()
	if got != nil {
		t.Errorf("got %v; want nil resolver by default", got)
	}
}

// ---------------------------------------------------------------------------
// SetPinned
// ---------------------------------------------------------------------------

func TestMockHistoryManager_SetPinned_ReturnsNil(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(3)
	err := m.SetPinned(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Save
// ---------------------------------------------------------------------------

func TestMockHistoryManager_Save_ReturnsNil(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(2)
	err := m.Save(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sync
// ---------------------------------------------------------------------------

func TestMockHistoryManager_Sync_ReturnsNil(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(2)
	err := m.Sync(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetFilePath
// ---------------------------------------------------------------------------

func TestMockHistoryManager_GetFilePath_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	path := m.GetFilePath()
	if path != "" {
		t.Errorf("got path=%q; want \"\"", path)
	}
}

// ---------------------------------------------------------------------------
// SetInternalContents
// ---------------------------------------------------------------------------

func TestMockHistoryManager_SetInternalContents_StoresContents(t *testing.T) {
	t.Parallel()

	m := newMockWithContents(2) // pre-seed with 2 entries

	replacement := seedContents(3) // replace with 3 different entries
	m.SetInternalContents(replacement)

	got := m.GetContents()
	if len(got) != 3 {
		t.Fatalf("got %d contents; want 3", len(got))
	}
	// Verify logical equivalence for each content (role and parts).
	for i, want := range replacement {
		if got[i].Role != want.Role {
			t.Errorf("content[%d].Role = %q; want %q", i, got[i].Role, want.Role)
		}
		if len(got[i].Parts) != len(want.Parts) {
			t.Errorf("content[%d] has %d parts; want %d", i, len(got[i].Parts), len(want.Parts))
		}
	}
}

func TestMockHistoryManager_SetInternalContents_DeepCopyCheck(t *testing.T) {
	t.Parallel()

	m := &MockHistoryManager{}
	replacement := seedContents(1)
	m.SetInternalContents(replacement)

	// GetContents returns a deep copy; mutating it must not affect internal state.
	got := m.GetContents()
	if len(got) != 1 {
		t.Fatalf("got %d contents; want 1", len(got))
	}
	got[0].Parts[0].Text = "mutated"

	gotAgain := m.GetContents()
	if gotAgain[0].Parts[0].Text == "mutated" {
		t.Error("SetInternalContents/GetContents did not deep-copy; internal state was mutated")
	}
}
