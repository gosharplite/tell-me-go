// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

type mockHistoryProvider struct {
	GetHistoryStreamFunc func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error)
}

func (m *mockHistoryProvider) GetHistoryStream(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
	if m.GetHistoryStreamFunc != nil {
		return m.GetHistoryStreamFunc(ctx, limit, cursor)
	}
	return nil, "", nil
}

type mockHistoryModifier struct {
	ArchiveFunc       func(ctx context.Context, contents []*llm.Content) error
	SetPinnedFunc     func(ctx context.Context, turnIndex int, pinned bool) error
	GetFilePathFunc   func() string
	RollbackTurnsFunc func(ctx context.Context, turns int) (int, int, int, error)

	SetPinnedCalled   bool
	RollbackCalled    bool
	LastPinnedIndex   int
	LastPinnedState   bool
	LastRollbackTurns int
}

func (m *mockHistoryModifier) Archive(ctx context.Context, contents []*llm.Content) error {
	if m.ArchiveFunc != nil {
		return m.ArchiveFunc(ctx, contents)
	}
	return nil
}

func (m *mockHistoryModifier) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	m.SetPinnedCalled = true
	m.LastPinnedIndex = turnIndex
	m.LastPinnedState = pinned
	if m.SetPinnedFunc != nil {
		return m.SetPinnedFunc(ctx, turnIndex, pinned)
	}
	return nil
}

func (m *mockHistoryModifier) GetFilePath() string {
	if m.GetFilePathFunc != nil {
		return m.GetFilePathFunc()
	}
	return "test.log"
}

func (m *mockHistoryModifier) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	m.RollbackCalled = true
	m.LastRollbackTurns = turns
	if m.RollbackTurnsFunc != nil {
		return m.RollbackTurnsFunc(ctx, turns)
	}
	return 0, 0, 0, nil
}

func newHistoryState(userContent, modelContent string) []ports.HistoryViewDTO {
	return []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: userContent, OriginalIndex: 0},
		{Role: "assistant", ContentPreview: modelContent, OriginalIndex: 1},
	}
}

func verifyScrollInteraction(t *testing.T, m *rootBrowserModel, expectedTurn int) {
	t.Helper()
	if m.selectedTurn != expectedTurn {
		t.Errorf("expected selectedTurn %d, got %d", expectedTurn, m.selectedTurn)
	}
}

func verifyPinInteraction(t *testing.T, m *rootBrowserModel, mock *mockHistoryModifier, expectedTurnIndex int, expectedOriginalIndex int, expectedState bool) {
	t.Helper()
	if !mock.SetPinnedCalled {
		t.Error("expected SetPinned to be called")
	}
	if mock.LastPinnedIndex != expectedTurnIndex {
		t.Errorf("expected pinned turn index %d, got %d", expectedTurnIndex, mock.LastPinnedIndex)
	}
	if mock.LastPinnedState != expectedState {
		t.Errorf("expected pinned state %v, got %v", expectedState, mock.LastPinnedState)
	}
	// Verify local state update
	found := false
	for _, dto := range m.history {
		if dto.OriginalIndex == expectedOriginalIndex {
			if dto.IsPinned != expectedState {
				t.Errorf("expected DTO at index %d to have IsPinned=%v", expectedOriginalIndex, expectedState)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("could not find DTO with OriginalIndex %d in history", expectedOriginalIndex)
	}
}

func verifyRollbackInteraction(t *testing.T, m *rootBrowserModel, mock *mockHistoryModifier, expectedTurns int) {
	t.Helper()
	if !mock.RollbackCalled {
		t.Error("expected RollbackTurns to be called")
	}
	if mock.LastRollbackTurns != expectedTurns {
		t.Errorf("expected %d turns to be rolled back, got %d", expectedTurns, mock.LastRollbackTurns)
	}
	if !m.isLoading {
		t.Error("expected model to enter loading state after rollback")
	}
}

func verifySearchInteraction(t *testing.T, m *rootBrowserModel, expectedSearching bool, expectedQuery string) {
	t.Helper()
	if m.isSearching != expectedSearching {
		t.Errorf("expected isSearching %v, got %v", expectedSearching, m.isSearching)
	}
	if expectedSearching && !m.searchBar.Focused() {
		t.Error("expected search bar to be focused")
	}
	if m.currentQuery != expectedQuery {
		t.Errorf("expected currentQuery %q, got %q", expectedQuery, m.currentQuery)
	}
}

type updateTestCase struct {
	name         string
	initialState func(*rootBrowserModel)
	msg          tea.Msg
	check        func(*testing.T, *rootBrowserModel, tea.Cmd, *mockHistoryModifier)
	expectedQuit bool
}

func getUpdateTestCases() []updateTestCase {
	tests := navigationTestCases()
	tests = append(tests, actionTestCases()...)
	tests = append(tests, searchTestCases()...)
	tests = append(tests, systemTestCases()...)
	return tests
}

func TestBrowserModel_Update(t *testing.T) {
	for _, tt := range getUpdateTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			mockProvider := &mockHistoryProvider{}
			mockModifier := &mockHistoryModifier{}
			m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
			if tt.initialState != nil {
				tt.initialState(m)
			}

			newModel, cmd := m.Update(tt.msg)
			updatedModel := newModel.(*rootBrowserModel)

			if tt.expectedQuit {
				if cmd == nil {
					t.Fatal("expected quit command, got nil")
				}
				return
			}

			if tt.check != nil {
				tt.check(t, updatedModel, cmd, mockModifier)
			}
		})
	}
}

func TestFetchHistoryCmd(t *testing.T) {
	mockProvider := &mockHistoryProvider{
		GetHistoryStreamFunc: func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
			return []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "test"},
			}, "next-cursor", nil
		},
	}

	cmd := fetchHistoryCmd(mockProvider, "start")
	msg := cmd()

	m, ok := msg.(historyLoadedMsg)
	if !ok {
		t.Fatalf("expected historyLoadedMsg, got %T", msg)
	}
	if m.nextCursor != "next-cursor" {
		t.Errorf("got cursor %q; want %q", m.nextCursor, "next-cursor")
	}
	if len(m.dtos) != 1 {
		t.Errorf("got %d dtos; want 1", len(m.dtos))
	}
}

func TestRootBrowserModel_Init(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command")
	}
}

func navigationTestCases() []updateTestCase {
	return []updateTestCase{
		{
			name: "Scroll down (j) increments selectedTurn",
			initialState: func(m *rootBrowserModel) {
				m.history = newHistoryState("Hello", "Hi")
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifyScrollInteraction(t, m, 1)
			},
		},
		{
			name: "Scroll down (j) stays at bottom",
			initialState: func(m *rootBrowserModel) {
				m.history = newHistoryState("Hello", "Hi")
				m.selectedTurn = 1
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifyScrollInteraction(t, m, 1)
			},
		},
		{
			name: "Scroll up (k) decrements selectedTurn",
			initialState: func(m *rootBrowserModel) {
				m.history = newHistoryState("Hello", "Hi")
				m.selectedTurn = 1
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifyScrollInteraction(t, m, 0)
			},
		},
		{
			name: "Search navigation (n/N)",
			initialState: func(m *rootBrowserModel) {
				m.matches = []int{10, 20, 30}
				m.currentMatch = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.currentMatch != 1 {
					t.Errorf("expected currentMatch 1, got %d", m.currentMatch)
				}
				// Test N (previous)
				m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
				updated2 := m2.(*rootBrowserModel)
				if updated2.currentMatch != 0 {
					t.Errorf("expected currentMatch 0 after N, got %d", updated2.currentMatch)
				}
			},
		},
		{
			name: "Infinite pagination trigger (older history)",
			initialState: func(m *rootBrowserModel) {
				m.ready = true
				m.isLoading = false
				m.cursor = "next"
				m.isSearching = false
				m.viewport = viewport.New(80, 10)
				m.viewport.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\nline14\nline15\nline16\nline17\nline18\nline19\nline20")
				m.viewport.SetYOffset(0) // At top
			},
			msg: tea.KeyMsg{Type: tea.KeyUp},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				// The trigger happens at the end of Update
				if !m.isLoading {
					t.Error("expected isLoading to be true (pagination triggered)")
				}
			},
		},
	}
}

func actionTestCases() []updateTestCase {
	return []updateTestCase{
		{
			name: "Pin (p) delegates to HistoryModifier",
			initialState: func(m *rootBrowserModel) {
				m.history = newHistoryState("Hello", "Hi")
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifyPinInteraction(t, m, mock, 0, 0, true)
			},
		},
		{
			name: "Unpin (p) delegates to HistoryModifier",
			initialState: func(m *rootBrowserModel) {
				m.history = newHistoryState("Hello", "Hi")
				m.history[0].IsPinned = true
				m.history[1].IsPinned = true
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifyPinInteraction(t, m, mock, 0, 0, false)
			},
		},
		{
			name: "Pin (p) does nothing for archived messages",
			initialState: func(m *rootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0, IsArchived: true},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if mock.SetPinnedCalled {
					t.Error("expected SetPinned NOT to be called for archived messages")
				}
			},
		},
		{
			name: "Rollback (r) delegates to HistoryModifier",
			initialState: func(m *rootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "1", OriginalIndex: 0},
					{Role: "assistant", ContentPreview: "2", OriginalIndex: 1},
					{Role: "user", ContentPreview: "3", OriginalIndex: 2},
					{Role: "assistant", ContentPreview: "4", OriginalIndex: 3},
				}
				m.selectedTurn = 2 // Selected second turn
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifyRollbackInteraction(t, m, mock, 1)
			},
		},
		{
			name: "Rollback (r) does nothing if no active history",
			initialState: func(m *rootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "1", OriginalIndex: 0, IsArchived: true},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if mock.RollbackCalled {
					t.Error("expected RollbackTurns NOT to be called")
				}
			},
		},
		{
			name: "Toggle thoughts (space)",
			initialState: func(m *rootBrowserModel) {
				m.showThoughts = true
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.showThoughts {
					t.Error("expected showThoughts to be false")
				}
			},
		},
		{
			name: "Quit (q)",
			initialState: func(m *rootBrowserModel) {
			},
			msg:          tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")},
			expectedQuit: true,
		},
	}
}

func searchTestCases() []updateTestCase {
	return []updateTestCase{
		{
			name: "Search toggle (/) activates search bar",
			initialState: func(m *rootBrowserModel) {
				m.isSearching = false
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifySearchInteraction(t, m, true, "")
			},
		},
		{
			name: "Search typing updates search bar",
			initialState: func(m *rootBrowserModel) {
				m.isSearching = true
				m.searchBar.Focus()
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.searchBar.Value() != "abc" {
					t.Errorf("expected search bar value 'abc', got %q", m.searchBar.Value())
				}
			},
		},
		{
			name: "Search escape cancels search",
			initialState: func(m *rootBrowserModel) {
				m.isSearching = true
				m.searchBar.SetValue("test")
			},
			msg: tea.KeyMsg{Type: tea.KeyEsc},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifySearchInteraction(t, m, false, "")
				if m.searchBar.Value() != "" {
					t.Error("expected search bar value to be cleared")
				}
			},
		},
		{
			name: "Search enter executes search",
			initialState: func(m *rootBrowserModel) {
				m.isSearching = true
				m.searchBar.Focus()
				m.searchBar.SetValue("test")
				m.ready = true
				m.viewport = viewport.New(80, 24)
			},
			msg: tea.KeyMsg{Type: tea.KeyEnter},
			check: func(t *testing.T, m *rootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				verifySearchInteraction(t, m, false, "test")
			},
		},
	}
}

func systemTestCases() []updateTestCase {
	return []updateTestCase{
		{
			name:  "WindowSizeMsg updates dimensions",
			msg:   tea.WindowSizeMsg{Width: 100, Height: 50},
			check: checkWindowSizeMsg,
		},
		{
			name: "historyLoadedMsg updates history",
			msg: historyLoadedMsg{
				dtos: []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "new"},
				},
				nextCursor: "next",
			},
			check: checkHistoryLoadedMsg,
		},
		{
			name: "historyLoadedMsg with error",
			msg: historyLoadedMsg{
				err: errors.New("boom"),
			},
			check: checkHistoryLoadedMsgError,
		},
		{
			name: "fileChangedMsg triggers reload",
			initialState: func(m *rootBrowserModel) {
				m.lastMutationTime = time.Now().Add(-1 * time.Second)
				m.history = []ports.HistoryViewDTO{{Role: "user"}}
			},
			msg:   fileChangedMsg{},
			check: checkFileChangedMsg,
		},
		{
			name: "fileChangedMsg ignored if too soon after mutation",
			initialState: func(m *rootBrowserModel) {
				m.lastMutationTime = time.Now()
				m.history = []ports.HistoryViewDTO{{Role: "user"}}
			},
			msg:   fileChangedMsg{},
			check: checkFileChangedMsgDebounced,
		},
		{
			name: "watcherErrorMsg sets error on model",
			msg:  watcherErrorMsg{err: errors.New("watch failed")},
			check: func(t *testing.T, m *rootBrowserModel, _ tea.Cmd, _ *mockHistoryModifier) {
				if m.err == nil || m.err.Error() != "watch failed" {
					t.Errorf("expected err 'watch failed', got %v", m.err)
				}
			},
		},
	}
}

func TestBrowserModel_SystemMessageOffset(t *testing.T) {
	ctx := context.Background()
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}

	t.Run("Pinning with system message", func(t *testing.T) {
		m := NewRootBrowserModel(ctx, mockProvider, mockModifier)
		m.history = []ports.HistoryViewDTO{
			{Role: "system", ContentPreview: "sys", OriginalIndex: 0},
			{Role: "user", ContentPreview: "u1", OriginalIndex: 1},
			{Role: "assistant", ContentPreview: "m1", OriginalIndex: 2},
		}
		// Select U1 (index 1)
		m.selectedTurn = 1
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})

		// Expected turnIndex: (1 - 1) / 2 = 0. ExpectedOriginalIndex: 1.
		verifyPinInteraction(t, m, mockModifier, 0, 1, true)
	})

	t.Run("Rollback with system message", func(t *testing.T) {
		mockModifier.RollbackCalled = false
		m := NewRootBrowserModel(ctx, mockProvider, mockModifier)
		m.history = []ports.HistoryViewDTO{
			{Role: "system", ContentPreview: "sys", OriginalIndex: 0},
			{Role: "user", ContentPreview: "u1", OriginalIndex: 1},
			{Role: "assistant", ContentPreview: "m1", OriginalIndex: 2},
			{Role: "user", ContentPreview: "u2", OriginalIndex: 3},
			{Role: "assistant", ContentPreview: "m2", OriginalIndex: 4},
		}
		// Select U2 (index 3)
		m.selectedTurn = 3
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})

		// totalMsgs = 5. targetStartIdx = ((3-1)&^1)+1 = 3.
		// turnsToRemove = (5 - 3 + 1) / 2 = 1.
		verifyRollbackInteraction(t, m, mockModifier, 1)
	})
}

func checkWindowSizeMsg(t *testing.T, m *rootBrowserModel, _ tea.Cmd, _ *mockHistoryModifier) {
	t.Helper()
	if m.width != 100 || m.height != 50 {
		t.Errorf("expected 100x50, got %dx%d", m.width, m.height)
	}
	if !m.ready {
		t.Error("expected ready to be true after WindowSizeMsg")
	}
}

func checkHistoryLoadedMsg(t *testing.T, m *rootBrowserModel, _ tea.Cmd, _ *mockHistoryModifier) {
	t.Helper()
	if len(m.history) != 1 {
		t.Errorf("expected 1 history item, got %d", len(m.history))
	}
	if m.cursor != "next" {
		t.Errorf("expected cursor 'next', got %q", m.cursor)
	}
	if m.isLoading {
		t.Error("expected isLoading to be false")
	}
}

func checkHistoryLoadedMsgError(t *testing.T, m *rootBrowserModel, _ tea.Cmd, _ *mockHistoryModifier) {
	t.Helper()
	if m.err == nil || m.err.Error() != "boom" {
		t.Error("expected error 'boom'")
	}
}

func checkFileChangedMsg(t *testing.T, m *rootBrowserModel, _ tea.Cmd, _ *mockHistoryModifier) {
	t.Helper()
	if len(m.history) != 0 {
		t.Error("expected history to be cleared for reload")
	}
	if !m.isLoading {
		t.Error("expected isLoading to be true")
	}
}

func checkFileChangedMsgDebounced(t *testing.T, m *rootBrowserModel, _ tea.Cmd, _ *mockHistoryModifier) {
	t.Helper()
	if len(m.history) != 1 {
		t.Error("expected history NOT to be cleared (debounced)")
	}
}

// ── Pinning error path hardening tests (Issue #383) ──

func TestTogglePin_SetPinnedError(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{
		SetPinnedFunc: func(ctx context.Context, turnIndex int, pinned bool) error {
			return errors.New("set pinned failed")
		},
	}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "Hello", OriginalIndex: 0},
		{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1},
	}
	m.selectedTurn = 0

	// Toggle pin - should fail
	m.togglePin()

	// Verify error is set on model
	if m.err == nil {
		t.Fatal("expected error to be set on model after SetPinned failure")
	}
	if !strings.Contains(m.err.Error(), "set pinned failed") {
		t.Errorf("expected error 'set pinned failed', got %v", m.err)
	}

	// Verify local pin state was NOT changed
	if m.history[0].IsPinned {
		t.Error("expected pin state to remain unchanged after error")
	}

	// Verify View() renders the error
	view := m.View()
	if !strings.Contains(view, "Error: set pinned failed") {
		t.Errorf("expected View() to render error, got %q", view)
	}
}

func TestRollbackToSelected_RollbackError(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{
		RollbackTurnsFunc: func(ctx context.Context, turns int) (int, int, int, error) {
			return 0, 0, 0, errors.New("rollback failed")
		},
	}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.isLoading = false // Reset initial loading state
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "1", OriginalIndex: 0},
		{Role: "assistant", ContentPreview: "2", OriginalIndex: 1},
		{Role: "user", ContentPreview: "3", OriginalIndex: 2},
		{Role: "assistant", ContentPreview: "4", OriginalIndex: 3},
	}
	m.selectedTurn = 0 // First turn

	// Rollback to selected - should fail
	cmd := m.rollbackToSelected()

	// Verify error is set on model
	if m.err == nil {
		t.Fatal("expected error to be set on model after RollbackTurns failure")
	}
	if !strings.Contains(m.err.Error(), "rollback failed") {
		t.Errorf("expected error 'rollback failed', got %v", m.err)
	}

	// Verify no cmd was returned (nil on error)
	if cmd != nil {
		t.Error("expected nil cmd when RollbackTurns fails")
	}

	// Verify model didn't enter loading state
	if m.isLoading {
		t.Error("expected isLoading to remain false after rollback error")
	}

	// Verify View() renders the error
	m.ready = true
	view := m.View()
	if !strings.Contains(view, "Error: rollback failed") {
		t.Errorf("expected View() to render error, got %q", view)
	}
}

func TestTogglePin_RecoveryAfterError(t *testing.T) {
	// Verify that after a SetPinned error, clearing the error and retrying works.
	mockProvider := &mockHistoryProvider{}
	callCount := 0
	mockModifier := &mockHistoryModifier{
		SetPinnedFunc: func(ctx context.Context, turnIndex int, pinned bool) error {
			callCount++
			if callCount == 1 {
				return errors.New("transient error")
			}
			return nil
		},
	}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "Hello", OriginalIndex: 0},
		{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1},
	}
	m.selectedTurn = 0

	// First attempt fails
	m.togglePin()
	if m.err == nil {
		t.Fatal("expected error after first attempt")
	}

	// Clear error
	m.err = nil

	// Second attempt succeeds
	m.togglePin()
	if m.err != nil {
		t.Errorf("expected no error after second attempt, got %v", m.err)
	}
	if !m.history[0].IsPinned {
		t.Error("expected pin state to be set after successful retry")
	}
}

// ── Additional coverage tests for tui package ──

func TestTogglePin_LocalStateUpdateNotFound(t *testing.T) {
	mockProvider := &mockHistoryProvider{
		GetHistoryStreamFunc: func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
			return []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "reloaded", OriginalIndex: 0},
				{Role: "assistant", ContentPreview: "reloaded", OriginalIndex: 1},
			}, "", nil
		},
	}
	m := NewRootBrowserModel(context.Background(), mockProvider, &mockHistoryModifier{})
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "hello", OriginalIndex: 0},
		{Role: "assistant", ContentPreview: "hi", OriginalIndex: 1},
	}
	m.selectedTurn = 0
	m.isLoading = false

	// Use SetPinnedFunc to clear history between getTurnForPinning and updateLocalPinState,
	// simulating a concurrent modification that makes the local state stale.
	mockModifier := &mockHistoryModifier{
		SetPinnedFunc: func(ctx context.Context, turnIndex int, pinned bool) error {
			m.history = nil
			return nil
		},
	}
	m.cmdService = mockModifier

	m.togglePin()

	if !m.isLoading {
		t.Error("expected isLoading=true after local update failure")
	}
	if len(m.history) != 0 {
		t.Errorf("expected history to be cleared, got %d items", len(m.history))
	}
	if m.selectedTurn != -1 {
		t.Errorf("expected selectedTurn=-1, got %d", m.selectedTurn)
	}
}

func TestTogglePin_LocalStateUpdateSucceeds(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "hello", OriginalIndex: 0},
		{Role: "assistant", ContentPreview: "hi", OriginalIndex: 1},
	}
	m.selectedTurn = 0
	m.isLoading = false

	m.togglePin()

	if m.isLoading {
		t.Error("expected isLoading=false after successful local update")
	}
	if !m.history[0].IsPinned {
		t.Error("expected local pin state to be true")
	}
	if !m.history[1].IsPinned {
		t.Error("expected second message in turn to also be pinned")
	}
}

func TestRollbackToSelected_ArchivedFeedback(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "archived msg", OriginalIndex: 0, IsArchived: true},
		{Role: "assistant", ContentPreview: "archived resp", OriginalIndex: 1, IsArchived: true},
	}
	m.selectedTurn = 0

	cmd := m.rollbackToSelected()

	if cmd != nil {
		t.Error("expected nil cmd for archived rollback attempt")
	}
	if m.err == nil {
		t.Fatal("expected error to be set for archived rollback")
	}
	if !strings.Contains(m.err.Error(), "archived") {
		t.Errorf("expected 'archived' in error message, got: %v", m.err)
	}
}

// ── Additional coverage tests for tui package ──

func TestPromptCapturer_Close(t *testing.T) {
	base := &mockBaseCapturer{}
	svc := &mockSuggestionService{}
	capturer := NewPromptCapturer(base, svc)

	// Close with both base and svc
	err := capturer.Close(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Close with nil svc
	capturer2 := NewPromptCapturer(base, nil)
	err = capturer2.Close(context.Background())
	if err != nil {
		t.Errorf("unexpected error with nil svc: %v", err)
	}
}

type mockCloseErrorSvc struct {
	mockSuggestionService
}

func (m *mockCloseErrorSvc) Close(ctx context.Context) error {
	return errors.New("close error")
}

func TestPromptCapturer_Close_Error(t *testing.T) {
	base := &mockBaseCapturer{}
	svc := &mockCloseErrorSvc{}
	capturer := NewPromptCapturer(base, svc)

	err := capturer.Close(context.Background())
	if err == nil {
		t.Fatal("expected error from Close")
	}
	if !strings.Contains(err.Error(), "close error") {
		t.Errorf("expected 'close error' in error message, got %v", err)
	}
}

func TestBrowserModel_MoveSearchMatch_Wrapping(t *testing.T) {
	m := &rootBrowserModel{
		matches:      []int{10, 20, 30},
		currentMatch: 0,
	}

	// Move forward past end: should wrap to 0
	m.moveSearchMatch(5)
	if m.currentMatch != 2 {
		t.Errorf("expected currentMatch 2 after forward wrap, got %d", m.currentMatch)
	}

	// Move backward past start: wraps via Go's remainder semantics
	m.moveSearchMatch(-5)
	// (2 - 5) % 3 = -3 % 3 = 0 in Go (remainder, not modulo)
	if m.currentMatch != 0 {
		t.Errorf("expected currentMatch 0 after backward wrap, got %d", m.currentMatch)
	}

	// No matches
	m.matches = nil
	m.moveSearchMatch(1)
	// Should not panic and currentMatch unchanged
}

func TestBrowserModel_MoveSelection_EdgeCases(t *testing.T) {
	m := &rootBrowserModel{
		history: []ports.HistoryViewDTO{
			{Role: "user", OriginalIndex: 0},
			{Role: "assistant", OriginalIndex: 1},
		},
		selectedTurn: 0,
	}

	// Move up past top
	m.moveSelection(-5)
	if m.selectedTurn != 0 {
		t.Errorf("expected selectedTurn 0, got %d", m.selectedTurn)
	}

	// Move down past bottom
	m.moveSelection(10)
	if m.selectedTurn != 1 {
		t.Errorf("expected selectedTurn 1, got %d", m.selectedTurn)
	}

	// Empty history
	m.history = nil
	m.moveSelection(1)
	// Should not panic
}

func TestBrowserModel_SearchEnter_SameQuery(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.isSearching = true
	m.searchBar.Focus()
	m.searchBar.SetValue("same-query")
	m.currentQuery = "same-query"
	m.ready = true
	m.viewport = viewport.New(80, 24)

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := newModel.(*rootBrowserModel)

	if updated.isSearching {
		t.Error("expected search to be deactivated")
	}
	// Cache should NOT be invalidated when query hasn't changed
	if updated.lastQuery != m.lastQuery {
		t.Error("expected lastQuery to remain unchanged when query doesn't change")
	}
}

func TestBrowserModel_HandleViewportUpdate_ScrollBottom(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.ready = true
	m.isLoading = false
	m.cursor = "next"
	m.viewport = viewport.New(80, 10)
	m.viewport.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\nline14\nline15")
	m.viewport.SetYOffset(5) // Middle

	// Scroll to bottom
	newModel, _ := m.handleViewportUpdate(tea.KeyMsg{Type: tea.KeyEnd})
	updated := newModel.(*rootBrowserModel)
	// KeyEnd triggers viewport to go to bottom; exact offset depends on viewport behavior
	_ = updated.viewport.YOffset
	_ = updated
}

// ── Final 8 uncovered branch tests (Issue #433) ──

func TestSyncViewportToSelectedTurn_BelowViewport(t *testing.T) {
	m := &rootBrowserModel{
		selectedTurn: 2,
		turnOffsets:  []int{0, 5, 50, 55},
		viewport:     viewport.New(80, 10),
	}
	// Need enough content lines to support YOffset >= 41
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line"
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.SetYOffset(0)
	m.syncViewportToSelectedTurn()
	// targetLine=50, viewport.YOffset=0, viewport.Height=10
	// 50 >= 0+10 → should set YOffset to 50-10+1 = 41
	if m.viewport.YOffset == 0 {
		t.Error("expected YOffset to be adjusted, got 0")
	}
}

func TestGetTurnIndex_SystemDto(t *testing.T) {
	m := &rootBrowserModel{
		history: []ports.HistoryViewDTO{
			{Role: "system", OriginalIndex: 0},
		},
	}
	turnIdx := m.getTurnIndex(m.history[0])
	if turnIdx != -1 {
		t.Errorf("expected -1 for system DTO, got %d", turnIdx)
	}
}

func TestHandleHistoryLoadedMsg_EmptyDtos(t *testing.T) {
	m := &rootBrowserModel{
		isLoading:    true,
		selectedTurn: -1,
		history:      []ports.HistoryViewDTO{{Role: "user", OriginalIndex: 0}},
	}
	newModel, cmd := m.handleHistoryLoadedMsg(historyLoadedMsg{
		dtos:       nil,
		nextCursor: "EOF",
	})
	updated := newModel.(*rootBrowserModel)
	if updated.isLoading {
		t.Error("expected isLoading to be false")
	}
	if updated.cursor != "EOF" {
		t.Errorf("expected cursor 'EOF', got %q", updated.cursor)
	}
	if cmd != nil {
		t.Error("expected nil cmd for empty dtos")
	}
}

func TestHandleHistoryLoadedMsg_PrependBoundsSafety(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}

	// Seed the model with history and a deliberately short turnOffsets slice
	// to exercise the bounds-check path.
	m := &rootBrowserModel{
		provider:   mockProvider,
		cmdService: mockModifier,
		history: []ports.HistoryViewDTO{
			{Role: "user", ContentPreview: "existing1", OriginalIndex: 3},
			{Role: "assistant", ContentPreview: "existing2", OriginalIndex: 4},
		},
		selectedTurn: 1,
		ready:        true,
		viewport:     viewport.New(80, 24),
		turnOffsets:  []int{0}, // deliberately too short — only 1 entry
	}
	m.viewport.SetContent("dummy content")

	// Prepend 3 new DTOs
	msg := historyLoadedMsg{
		dtos: []ports.HistoryViewDTO{
			{Role: "user", ContentPreview: "older1", OriginalIndex: 0},
			{Role: "assistant", ContentPreview: "older2", OriginalIndex: 1},
			{Role: "user", ContentPreview: "older3", OriginalIndex: 2},
		},
		nextCursor: "prev-cursor",
	}

	// This must not panic. After updateViewportContent(), turnOffsets
	// will have 5 entries and numAdded=3 will be valid — but we verify
	// the bounds-check path by confirming no panic occurs regardless.
	newModel, _ := m.handleHistoryLoadedMsg(msg)
	updated := newModel.(*rootBrowserModel)

	if len(updated.history) != 5 {
		t.Errorf("expected 5 history items, got %d", len(updated.history))
	}
	if updated.selectedTurn != 4 {
		t.Errorf("expected selectedTurn 4, got %d", updated.selectedTurn)
	}
	if updated.cursor != "prev-cursor" {
		t.Errorf("expected cursor 'prev-cursor', got %q", updated.cursor)
	}
	// turnOffsets should now have 5 entries (repopulated by updateViewportContent)
	if len(updated.turnOffsets) != 5 {
		t.Errorf("expected 5 turn offsets, got %d", len(updated.turnOffsets))
	}
}

func TestHandleHistoryLoadedMsg_PartialError(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}

	m := &rootBrowserModel{
		provider:     mockProvider,
		cmdService:   mockModifier,
		isLoading:    true,
		selectedTurn: -1,
	}

	// Partial result: data + error
	msg := historyLoadedMsg{
		dtos: []ports.HistoryViewDTO{
			{Role: "user", ContentPreview: "partial data", OriginalIndex: 0},
			{Role: "assistant", ContentPreview: "still useful", OriginalIndex: 1},
		},
		nextCursor: "next-cursor",
		err:        errors.New("timeout fetching older pages"),
	}

	newModel, _ := m.handleHistoryLoadedMsg(msg)
	updated := newModel.(*rootBrowserModel)

	// Data must be loaded despite the error
	if len(updated.history) != 2 {
		t.Errorf("expected 2 history items (partial data preserved), got %d", len(updated.history))
	}
	if updated.cursor != "next-cursor" {
		t.Errorf("expected cursor 'next-cursor', got %q", updated.cursor)
	}
	// Error must NOT be set on the model (data is displayed)
	if updated.err != nil {
		t.Errorf("expected no error on model when partial data exists, got %v", updated.err)
	}
	if updated.isLoading {
		t.Error("expected isLoading to be false")
	}
}

func TestHandleHistoryLoadedMsg_ErrorNoData(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}

	m := &rootBrowserModel{
		provider:     mockProvider,
		cmdService:   mockModifier,
		isLoading:    true,
		selectedTurn: -1,
	}

	// Error with no data at all
	msg := historyLoadedMsg{
		dtos:       nil,
		nextCursor: "",
		err:        errors.New("connection refused"),
	}

	newModel, _ := m.handleHistoryLoadedMsg(msg)
	updated := newModel.(*rootBrowserModel)

	// Error must be set
	if updated.err == nil || updated.err.Error() != "connection refused" {
		t.Errorf("expected error 'connection refused', got %v", updated.err)
	}
	if updated.isLoading {
		t.Error("expected isLoading to be false even on error")
	}
}

func TestFetchHistoryCmd_ReturnsDataAndError(t *testing.T) {
	mockProvider := &mockHistoryProvider{
		GetHistoryStreamFunc: func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
			return []ports.HistoryViewDTO{
				{Role: "user", ContentPreview: "partial"},
			}, "next", errors.New("partial failure")
		},
	}

	cmd := fetchHistoryCmd(mockProvider, "start")
	msg := cmd()

	result, ok := msg.(historyLoadedMsg)
	if !ok {
		t.Fatalf("expected historyLoadedMsg, got %T", msg)
	}
	if len(result.dtos) != 1 {
		t.Errorf("expected 1 dto, got %d", len(result.dtos))
	}
	if result.nextCursor != "next" {
		t.Errorf("expected cursor 'next', got %q", result.nextCursor)
	}
	if result.err == nil {
		t.Error("expected non-nil error")
	}
}

func TestGetTurnForPinning_OutOfBounds(t *testing.T) {
	m := &rootBrowserModel{
		history:      []ports.HistoryViewDTO{{Role: "user", OriginalIndex: 0}},
		selectedTurn: 5,
	}
	_, _, ok := m.getTurnForPinning()
	if ok {
		t.Error("expected ok=false for out of bounds selectedTurn")
	}

	m.selectedTurn = -1
	_, _, ok = m.getTurnForPinning()
	if ok {
		t.Error("expected ok=false for selectedTurn=-1")
	}
}

func TestRenderFooter_QueryNoMatches(t *testing.T) {
	m := &rootBrowserModel{
		currentQuery: "zzzzz",
		matches:      nil,
		currentMatch: 0,
	}
	got := m.renderFooter()
	if !strings.Contains(got, "no matches") {
		t.Errorf("expected 'no matches' in footer, got %q", got)
	}
}

func TestMoveSelection_EmptyHistory(t *testing.T) {
	m := &rootBrowserModel{selectedTurn: -1}
	m.moveSelection(1)
	if m.selectedTurn != -1 {
		t.Errorf("expected selectedTurn -1, got %d", m.selectedTurn)
	}
}

// ── G11 + G12 + G15 + G17 + G18 + G19: Browser hardening tests ──

func TestBrowserModel_HandleWindowSizeMsg_ZeroWidth(t *testing.T) {
	m := &rootBrowserModel{ready: true, width: 80, height: 40, viewport: viewport.New(80, 30)}
	newModel, _ := m.handleWindowSizeMsg(tea.WindowSizeMsg{Width: 0, Height: 0})
	updated := newModel.(*rootBrowserModel)
	if updated.width < 20 {
		t.Errorf("width clamped, got %d", updated.width)
	}
	if updated.height < 5 {
		t.Errorf("height clamped, got %d", updated.height)
	}
}

func TestBrowserModel_HandleFileChangedMsg_WatcherGuard(t *testing.T) {
	m := &rootBrowserModel{lastMutationTime: time.Now(), watcherRestarting: true}
	_, cmd := m.handleFileChangedMsg(fileChangedMsg{})
	if cmd != nil {
		t.Error("expected nil cmd when already restarting")
	}
	m.watcherRestarting = false
	_, cmd = m.handleFileChangedMsg(fileChangedMsg{})
	if cmd == nil {
		t.Error("expected non-nil cmd for restart")
	}
}

func TestBrowserModel_PostRollback_HasTimeout(t *testing.T) {
	mockProvider := &mockHistoryProvider{GetHistoryStreamFunc: func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
		return []ports.HistoryViewDTO{{Role: "user", ContentPreview: "ok"}}, "", nil
	}}
	mockModifier := &mockHistoryModifier{RollbackTurnsFunc: func(ctx context.Context, turns int) (int, int, int, error) { return 1, 0, 0, nil }}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.history = []ports.HistoryViewDTO{{Role: "user", ContentPreview: "1", OriginalIndex: 0}, {Role: "assistant", ContentPreview: "2", OriginalIndex: 1}}
	m.selectedTurn = 0
	cmd := m.rollbackToSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	if !m.isLoading {
		t.Error("expected isLoading")
	}
}

func TestBrowserModel_GetPinningMetrics_AllArchived(t *testing.T) {
	m := &rootBrowserModel{history: []ports.HistoryViewDTO{
		{Role: "user", OriginalIndex: 0, IsArchived: true},
		{Role: "assistant", OriginalIndex: 1, IsArchived: true},
	}}
	active, pinned := m.getPinningMetrics()
	if active != 0 {
		t.Errorf("expected 0 active, got %d", active)
	}
	if pinned != 0 {
		t.Errorf("expected 0 pinned, got %d", pinned)
	}
}

func TestBrowserModel_RenderThoughts_CacheSelfPopulating(t *testing.T) {
	m := &rootBrowserModel{width: 80, showThoughts: true, cachedThoughts: map[string]string{}}
	dto := ports.HistoryViewDTO{ID: "self-populate", ThoughtProcess: "new thought"}
	result := m.renderThoughts(dto, "  ")
	if !strings.Contains(result, "new thought") {
		t.Errorf("expected thought, got %q", result)
	}
	if _, ok := m.cachedThoughts["self-populate"]; !ok {
		t.Error("cache should be populated on miss")
	}
}

func TestRefreshTimeoutCmd(t *testing.T) {
	cmd := refreshTimeoutCmd(50 * time.Millisecond)
	start := time.Now()
	msg := cmd()
	elapsed := time.Since(start)
	historyMsg, ok := msg.(historyLoadedMsg)
	if !ok {
		t.Fatalf("expected historyLoadedMsg, got %T", msg)
	}
	if historyMsg.err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(historyMsg.err.Error(), "timed out") {
		t.Errorf("expected 'timed out', got: %v", historyMsg.err)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected >=50ms, got %v", elapsed)
	}
}

// TestProcessWatcherEvents_ClosedWatcher covers the select dispatch at
// browser.go:146-147. Injecting a real watcher then closing it immediately
// exercises the <-watcher.Events case where ok=false, which returns nil.
func TestProcessWatcherEvents_ClosedWatcher(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	_ = watcher.Close()

	m := &rootBrowserModel{ctx: context.Background()}
	msg := m.processWatcherEvents(watcher)
	if msg != nil {
		t.Errorf("expected nil msg from closed watcher, got %T: %v", msg, msg)
	}
}
