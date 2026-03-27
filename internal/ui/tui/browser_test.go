// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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

func TestBrowserModel_Update(t *testing.T) {
	tests := []struct {
		name                string
		initialState        func(*RootBrowserModel)
		msg                 tea.Msg
		check               func(*testing.T, RootBrowserModel, tea.Cmd, *mockHistoryModifier)
		expectedQuit        bool
		expectMutationCalls bool
	}{
		{
			name: "Scroll down (j) increments selectedTurn",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0},
					{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.selectedTurn != 1 {
					t.Errorf("expected selectedTurn 1, got %d", m.selectedTurn)
				}
			},
		},
		{
			name: "Scroll down (j) stays at bottom",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0},
					{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1},
				}
				m.selectedTurn = 1
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.selectedTurn != 1 {
					t.Errorf("expected selectedTurn 1, got %d", m.selectedTurn)
				}
			},
		},
		{
			name: "Scroll up (k) decrements selectedTurn",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0},
					{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1},
				}
				m.selectedTurn = 1
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.selectedTurn != 0 {
					t.Errorf("expected selectedTurn 0, got %d", m.selectedTurn)
				}
			},
		},
		{
			name: "Pin (p) delegates to HistoryModifier",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0, IsPinned: false},
					{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1, IsPinned: false},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if !mock.SetPinnedCalled {
					t.Error("expected SetPinned to be called")
				}
				if mock.LastPinnedIndex != 0 {
					t.Errorf("expected pinned index 0, got %d", mock.LastPinnedIndex)
				}
				if !mock.LastPinnedState {
					t.Error("expected pinned state true")
				}
				// Verify local state update
				if !m.history[0].IsPinned || !m.history[1].IsPinned {
					t.Error("expected both DTOs in turn to be pinned locally")
				}
			},
		},
		{
			name: "Unpin (p) delegates to HistoryModifier",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0, IsPinned: true},
					{Role: "assistant", ContentPreview: "Hi", OriginalIndex: 1, IsPinned: true},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if !mock.SetPinnedCalled {
					t.Error("expected SetPinned to be called")
				}
				if mock.LastPinnedState {
					t.Error("expected pinned state false")
				}
				// Verify local state update
				if m.history[0].IsPinned || m.history[1].IsPinned {
					t.Error("expected both DTOs in turn to be unpinned locally")
				}
			},
		},
		{
			name: "Pin (p) does nothing for archived messages",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "Hello", OriginalIndex: 0, IsArchived: true},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if mock.SetPinnedCalled {
					t.Error("expected SetPinned NOT to be called for archived messages")
				}
			},
		},
		{
			name: "Rollback (r) delegates to HistoryModifier",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "1", OriginalIndex: 0},
					{Role: "assistant", ContentPreview: "2", OriginalIndex: 1},
					{Role: "user", ContentPreview: "3", OriginalIndex: 2},
					{Role: "assistant", ContentPreview: "4", OriginalIndex: 3},
				}
				m.selectedTurn = 2 // Selected second turn
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if !mock.RollbackCalled {
					t.Error("expected RollbackTurns to be called")
				}
				if mock.LastRollbackTurns != 1 {
					t.Errorf("expected 1 turn to be rolled back, got %d", mock.LastRollbackTurns)
				}
				if !m.isLoading {
					t.Error("expected model to enter loading state after rollback")
				}
			},
		},
		{
			name: "Rollback (r) does nothing if no active history",
			initialState: func(m *RootBrowserModel) {
				m.history = []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "1", OriginalIndex: 0, IsArchived: true},
				}
				m.selectedTurn = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if mock.RollbackCalled {
					t.Error("expected RollbackTurns NOT to be called")
				}
			},
		},
		{
			name: "Search toggle (/) activates search bar",
			initialState: func(m *RootBrowserModel) {
				m.isSearching = false
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if !m.isSearching {
					t.Error("expected isSearching to be true")
				}
				if !m.searchBar.Focused() {
					t.Error("expected search bar to be focused")
				}
			},
		},
		{
			name: "Search typing updates search bar",
			initialState: func(m *RootBrowserModel) {
				m.isSearching = true
				m.searchBar.Focus()
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.searchBar.Value() != "abc" {
					t.Errorf("expected search bar value 'abc', got %q", m.searchBar.Value())
				}
			},
		},
		{
			name: "Search escape cancels search",
			initialState: func(m *RootBrowserModel) {
				m.isSearching = true
				m.searchBar.SetValue("test")
			},
			msg: tea.KeyMsg{Type: tea.KeyEsc},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.isSearching {
					t.Error("expected isSearching to be false")
				}
				if m.searchBar.Value() != "" {
					t.Error("expected search bar value to be cleared")
				}
			},
		},
		{
			name: "Search enter executes search",
			initialState: func(m *RootBrowserModel) {
				m.isSearching = true
				m.searchBar.Focus()
				m.searchBar.SetValue("test")
				m.ready = true
				m.viewport = viewport.New(80, 24)
			},
			msg: tea.KeyMsg{Type: tea.KeyEnter},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.isSearching {
					t.Error("expected isSearching to be false after enter")
				}
				if m.currentQuery != "test" {
					t.Errorf("expected currentQuery 'test', got %q", m.currentQuery)
				}
			},
		},
		{
			name: "Search navigation (n/N)",
			initialState: func(m *RootBrowserModel) {
				m.matches = []int{10, 20, 30}
				m.currentMatch = 0
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.currentMatch != 1 {
					t.Errorf("expected currentMatch 1, got %d", m.currentMatch)
				}
				// Test N (previous)
				m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
				updated2 := m2.(RootBrowserModel)
				if updated2.currentMatch != 0 {
					t.Errorf("expected currentMatch 0 after N, got %d", updated2.currentMatch)
				}
			},
		},
		{
			name: "Toggle thoughts (space)",
			initialState: func(m *RootBrowserModel) {
				m.showThoughts = true
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.showThoughts {
					t.Error("expected showThoughts to be false")
				}
			},
		},
		{
			name: "Quit (q)",
			initialState: func(m *RootBrowserModel) {
			},
			msg:          tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")},
			expectedQuit: true,
		},
		{
			name: "WindowSizeMsg updates dimensions",
			msg:  tea.WindowSizeMsg{Width: 100, Height: 50},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.width != 100 || m.height != 50 {
					t.Errorf("expected 100x50, got %dx%d", m.width, m.height)
				}
				if !m.ready {
					t.Error("expected ready to be true after WindowSizeMsg")
				}
			},
		},
		{
			name: "historyLoadedMsg updates history",
			msg: historyLoadedMsg{
				dtos: []ports.HistoryViewDTO{
					{Role: "user", ContentPreview: "new"},
				},
				nextCursor: "next",
			},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if len(m.history) != 1 {
					t.Errorf("expected 1 history item, got %d", len(m.history))
				}
				if m.cursor != "next" {
					t.Errorf("expected cursor 'next', got %q", m.cursor)
				}
				if m.isLoading {
					t.Error("expected isLoading to be false")
				}
			},
		},
		{
			name: "historyLoadedMsg with error",
			msg: historyLoadedMsg{
				err: errors.New("boom"),
			},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if m.err == nil || m.err.Error() != "boom" {
					t.Error("expected error 'boom'")
				}
			},
		},
		{
			name: "fileChangedMsg triggers reload",
			initialState: func(m *RootBrowserModel) {
				m.lastMutationTime = time.Now().Add(-1 * time.Second)
				m.history = []ports.HistoryViewDTO{{Role: "user"}}
			},
			msg: fileChangedMsg{},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if len(m.history) != 0 {
					t.Error("expected history to be cleared for reload")
				}
				if !m.isLoading {
					t.Error("expected isLoading to be true")
				}
			},
		},
		{
			name: "fileChangedMsg ignored if too soon after mutation",
			initialState: func(m *RootBrowserModel) {
				m.lastMutationTime = time.Now()
				m.history = []ports.HistoryViewDTO{{Role: "user"}}
			},
			msg: fileChangedMsg{},
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				if len(m.history) != 1 {
					t.Error("expected history NOT to be cleared (debounced)")
				}
			},
		},
		{
			name: "Infinite pagination trigger",
			initialState: func(m *RootBrowserModel) {
				m.ready = true
				m.isLoading = false
				m.cursor = "next"
				m.isSearching = false
				m.viewport = viewport.New(80, 10)
				m.viewport.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\nline14\nline15\nline16\nline17\nline18\nline19\nline20")
				m.viewport.GotoBottom()
			},
			msg: tea.KeyMsg{Type: tea.KeyDown}, // Any msg that updates viewport might trigger it
			check: func(t *testing.T, m RootBrowserModel, cmd tea.Cmd, mock *mockHistoryModifier) {
				// The trigger happens at the end of Update
				if !m.isLoading {
					t.Error("expected isLoading to be true (pagination triggered)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockProvider := &mockHistoryProvider{}
			mockModifier := &mockHistoryModifier{}
			m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
			if tt.initialState != nil {
				tt.initialState(m)
			}

			newModel, cmd := m.Update(tt.msg)
			updatedModel := newModel.(RootBrowserModel)

			if tt.expectedQuit {
				if cmd == nil {
					t.Fatal("expected quit command, got nil")
				}
				// We can't easily execute tea.Quit and check it here as it's an internal tea message
				return
			}

			if tt.check != nil {
				tt.check(t, updatedModel, cmd, mockModifier)
			}
		})
	}
}

func TestBrowserModel_View(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	ctx := context.Background()
	m := NewRootBrowserModel(ctx, mockProvider, mockModifier)

	// Test Initializing state
	m.ready = false
	got := m.View()
	if !strings.Contains(got, "Initializing terminal...") {
		t.Errorf("expected Initializing state, got %q", got)
	}

	// Test ready state with data
	m.ready = true
	m.width = 80
	m.height = 40 // Larger height to ensure all items are in view
	m.viewport = viewport.New(80, 30)
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "User Message", OriginalIndex: 0, IsPinned: true},
		{Role: "assistant", ContentPreview: "Assistant Message", ThoughtProcess: "Thinking...", ToolCalls: []string{"test-tool"}, OriginalIndex: 1},
		{Role: "other", ContentPreview: "Other Message", OriginalIndex: 2},
		{Role: "user", ContentPreview: "Archived Message", IsArchived: true, OriginalIndex: 3},
	}
	m.selectedTurn = 0
	m.showThoughts = true

	// Trigger render
	rendered := m.renderHistory()
	m.viewport.SetContent(rendered)

	_ = m.View()

	// Assertions on the rendered content (directly or via View if height permits)
	expectedSubstrings := []string{
		"> [USER] - 1",
		"[PINNED]",
		"User Message",
		"[MODEL] - 1",
		"Assistant Message",
		"[THOUGHTS] Thinking...",
		"Executing tool: test-tool",
		"[OTHER] - 2",
		"(archived)",
		"Archived Message",
	}

	for _, s := range expectedSubstrings {
		if !strings.Contains(rendered, s) {
			t.Errorf("expected %q in renderHistory output", s)
		}
	}

	// Test Search state
	m.isSearching = true
	m.searchBar.SetValue("query")
	got = m.View()
	if !strings.Contains(got, "query") {
		t.Errorf("expected search bar view in search state, got %q", got)
	}

	// Test Error state
	m.err = errors.New("test error")
	got = m.View()
	if !strings.Contains(got, "Error: test error") {
		t.Errorf("expected error view, got %q", got)
	}
}

func TestBrowserModel_RenderHistory_Paths(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)

	m.isLoading = true
	got := m.renderHistory()
	if !strings.Contains(got, "Loading history...") {
		t.Errorf("expected Loading history..., got %q", got)
	}

	m.isLoading = false
	m.cursor = "EOF"
	got = m.renderHistory()
	if !strings.Contains(got, "No history found.") {
		t.Errorf("expected No history found., got %q", got)
	}

	// Multi-line content
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "Line 1\nLine 2", OriginalIndex: 0},
	}
	got = m.renderHistory()
	if !strings.Contains(got, "Line 1") || !strings.Contains(got, "Line 2") {
		t.Errorf("expected multi-line content, got %q", got)
	}

	// Loading more messages at bottom
	m.isLoading = true
	got = m.renderHistory()
	if !strings.Contains(got, "Loading more messages...") {
		t.Errorf("expected Loading more messages..., got %q", got)
	}

	// End of History at bottom
	m.isLoading = false
	m.cursor = "EOF"
	got = m.renderHistory()
	if !strings.Contains(got, "End of History") {
		t.Errorf("expected End of History, got %q", got)
	}

	// Highlight in renderHistory
	m.currentQuery = "Line"
	_ = m.renderHistory()
	if len(m.matches) == 0 {
		t.Error("expected matches to be populated")
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

func TestWatchHistoryFileCmd(t *testing.T) {
	// 1. Empty filepath
	cmd := watchHistoryFileCmd(context.Background(), "")
	if cmd() != nil {
		t.Error("expected nil msg for empty filepath")
	}

	// 2. Non-existent file
	cmd = watchHistoryFileCmd(context.Background(), "non-existent")
	if cmd() != nil {
		t.Error("expected nil msg for non-existent file")
	}

	// 3. Cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFilePath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd = watchHistoryFileCmd(ctx, tmpFilePath)
	if cmd() != nil {
		t.Error("expected nil msg for cancelled context")
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

func TestBrowserModel_HighlightMatches(t *testing.T) {
	m := &RootBrowserModel{}
	text := "This is a test message"
	query := "test"

	got := m.highlightMatches(text, query)
	if !strings.Contains(got, "test") {
		t.Errorf("expected highlight in %q", got)
	}

	// Test empty query
	if m.highlightMatches(text, "") != text {
		t.Error("expected original text for empty query")
	}

	// Test invalid regex query
	if m.highlightMatches(text, "[") != text {
		t.Error("expected original text for invalid regex query")
	}
}

func TestBrowserModel_Footer(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)

	// Test normal footer
	m.history = []ports.HistoryViewDTO{
		{Role: "user", OriginalIndex: 0},
		{Role: "assistant", OriginalIndex: 1},
	}
	got := m.renderFooter()
	if !strings.Contains(got, "Scroll") {
		t.Errorf("expected normal footer, got %q", got)
	}

	// Test high pinning pressure (count of pinned turns > 5)
	m.history = []ports.HistoryViewDTO{
		{Role: "user", OriginalIndex: 0, IsPinned: true},
		{Role: "assistant", OriginalIndex: 1, IsPinned: true},
		{Role: "user", OriginalIndex: 2, IsPinned: true},
		{Role: "assistant", OriginalIndex: 3, IsPinned: true},
		{Role: "user", OriginalIndex: 4, IsPinned: true},
		{Role: "assistant", OriginalIndex: 5, IsPinned: true},
		{Role: "user", OriginalIndex: 6, IsPinned: true},
		{Role: "assistant", OriginalIndex: 7, IsPinned: true},
		{Role: "user", OriginalIndex: 8, IsPinned: true},
		{Role: "assistant", OriginalIndex: 9, IsPinned: true},
		{Role: "user", OriginalIndex: 10, IsPinned: true},
		{Role: "assistant", OriginalIndex: 11, IsPinned: true},
	}
	got = m.renderFooter()
	if !strings.Contains(got, "High Pinning Pressure") {
		t.Errorf("expected high pinning pressure warning, got %q", got)
	}

	if m.calculateFooterHeight() != 2 {
		t.Errorf("expected footer height 2 for high pinning pressure, got %d", m.calculateFooterHeight())
	}

	// Test search query in footer
	m.currentQuery = "findme"
	m.matches = []int{1, 2}
	m.currentMatch = 0
	got = m.renderFooter()
	if !strings.Contains(got, "Query: \"findme\" (1/2 matches)") {
		t.Errorf("expected search info in footer, got %q", got)
	}

	// Test search query in footer with no matches
	m.matches = nil
	got = m.renderFooter()
	if !strings.Contains(got, "no matches") {
		t.Errorf("expected 'no matches' in footer, got %q", got)
	}
}
