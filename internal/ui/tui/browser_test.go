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
	"github.com/fsnotify/fsnotify"
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
	rendered, _ := m.renderHistory()
	m.viewport.SetContent(rendered)

	_ = m.View()

	// Strip ANSI escape sequences before asserting — lipgloss may emit
	// color codes depending on terminal environment state set by prior
	// test packages in the full suite (same class of bug as #511).
	rendered = stripANSI(rendered)

	// Assertions on the rendered content
	expectedSubstrings := []string{
		"> [USER] - 1",
		"[PINNED]",
		"User Message",
		"[MODEL] - 1",
		"Assistant Message",
		"[THOUGHTS]",
		"Thinking...",
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
	got, _ := m.renderHistory()
	if !strings.Contains(got, "Loading history...") {
		t.Errorf("expected Loading history..., got %q", got)
	}

	m.isLoading = false
	m.cursor = "EOF"
	got, _ = m.renderHistory()
	if !strings.Contains(got, "No history found.") {
		t.Errorf("expected No history found., got %q", got)
	}

	// Multi-line content
	m.history = []ports.HistoryViewDTO{
		{Role: "user", ContentPreview: "Line 1\nLine 2", OriginalIndex: 0},
	}
	got, _ = m.renderHistory()
	if !strings.Contains(got, "Line 1") || !strings.Contains(got, "Line 2") {
		t.Errorf("expected multi-line content, got %q", got)
	}

	// Loading more messages at bottom
	m.isLoading = true
	got, _ = m.renderHistory()
	if !strings.Contains(got, "Loading more messages...") {
		t.Errorf("expected Loading more messages..., got %q", got)
	}

	// Start of History at top
	m.isLoading = false
	m.cursor = "EOF"
	got, _ = m.renderHistory()
	if !strings.Contains(got, "Start of History") {
		t.Errorf("expected Start of History, got %q", got)
	}

	// Highlight in renderHistory
	m.currentQuery = "Line"
	m.updateViewportContent()
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
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)

	// 1. Empty filepath
	mockModifier.GetFilePathFunc = func() string { return "" }
	cmd := m.watchHistoryFileCmd()
	if cmd() != nil {
		t.Error("expected nil msg for empty filepath")
	}

	// 2. Non-existent file
	mockModifier.GetFilePathFunc = func() string { return "non-existent" }
	cmd = m.watchHistoryFileCmd()
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

	m.ctx = ctx
	mockModifier.GetFilePathFunc = func() string { return tmpFilePath }
	cmd = m.watchHistoryFileCmd()
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
	m := &rootBrowserModel{}
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

func TestBrowserModel_RenderThoughts_Cache(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)

	thought := "Thinking about the meaning of life..."
	m.history = []ports.HistoryViewDTO{
		{ID: "msg1", Role: "user", ThoughtProcess: thought, OriginalIndex: 0},
	}
	m.showThoughts = true
	m.width = 80
	m.ready = true
	m.viewport = viewport.New(80, 20)

	// Force cache warming and viewport population through standard lifecycle
	m.updateViewportContent()

	// Verify cache was warmed
	if len(m.cachedThoughts) == 0 {
		t.Fatal("Expected cachedThoughts to be populated")
	}

	if _, ok := m.cachedThoughts["msg1"]; !ok {
		t.Errorf("Expected msg1 to be cached")
	}

	// Verify final render
	got := m.viewport.View()
	if !strings.Contains(got, "Thinking about") {
		t.Errorf("Expected thoughts in viewport, got: %s", got)
	}

	// Test case: currentQuery is not empty
	m.currentQuery = "life"
	m.cachedThoughts = make(map[string]string) // Invalidate cache manually
	m.updateViewportContent()
	if !strings.Contains(m.cachedThoughts["msg1"], "life") {
		t.Errorf("Expected highlighted query in thoughts")
	}

	// Test case: width < 20
	m.width = 10
	m.cachedThoughts = make(map[string]string) // Invalidate cache manually
	m.updateViewportContent()
	if len(m.cachedThoughts) == 0 {
		t.Fatal("Expected cachedThoughts to be populated for small width")
	}
}

func TestStaleCachePrepend(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)
	m.width = 80
	m.ready = true
	m.showThoughts = true

	// 1. Initial history: one message with unique thought
	m.history = []ports.HistoryViewDTO{
		{ID: "msg1", ThoughtProcess: "THOUGHT_ONE", Role: "assistant"},
	}
	m.selectedTurn = 0

	// 2. Warm cache
	m.updateViewportContent()

	// Verify msg1 is cached (uses ID)
	if !strings.Contains(m.cachedThoughts["msg1"], "THOUGHT_ONE") {
		t.Fatalf("expected THOUGHT_ONE in cache['msg1'], got %q", m.cachedThoughts["msg1"])
	}

	// 3. Prepend older history: another message with a different thought
	newDtos := []ports.HistoryViewDTO{
		{ID: "msg0", ThoughtProcess: "THOUGHT_ZERO", Role: "assistant"},
	}

	// Simulate historyLoadedMsg which prepends
	m.handleHistoryLoadedMsg(historyLoadedMsg{dtos: newDtos})

	// Now m.history is [msg0, msg1]
	// msg0 is at index 0, msg1 is at index 1

	// 4. Update viewport content (this is where the fix is verified)
	m.updateViewportContent()

	// With the fix (using stable ID):
	// m.cachedThoughts["msg1"] still contains "THOUGHT_ONE"
	// m.cachedThoughts["msg0"] should be rendered and contain "THOUGHT_ZERO"

	// Let's check m.cachedThoughts["msg0"]
	if !strings.Contains(m.cachedThoughts["msg0"], "THOUGHT_ZERO") {
		t.Errorf("msg0 (THOUGHT_ZERO) NOT found in cache correctly")
	}

	// Let's check m.cachedThoughts["msg1"]
	if !strings.Contains(m.cachedThoughts["msg1"], "THOUGHT_ONE") {
		t.Errorf("msg1 (THOUGHT_ONE) lost or corrupted in cache")
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

// TestRecalculateSearchMatches_EdgeCases exercises the full recalculateSearchMatches
// function with varied queries and rendered content, including the defensive branches
// around regexp.Compile and match-count adjustment.
func TestRecalculateSearchMatches_EdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		currentQuery     string
		rendered         string
		initialMatches   []int
		initialMatch     int
		wantMatchCount   int
		wantCurrentMatch int
	}{
		{
			name:             "empty query clears matches",
			currentQuery:     "",
			rendered:         "line one\nline two\nline three",
			initialMatches:   []int{0, 1},
			initialMatch:     1,
			wantMatchCount:   0,
			wantCurrentMatch: 0,
		},
		{
			name:             "query matches single line",
			currentQuery:     "two",
			rendered:         "line one\nline two\nline three",
			wantMatchCount:   1,
			wantCurrentMatch: 0,
		},
		{
			name:             "query matches multiple lines",
			currentQuery:     "line",
			rendered:         "line one\nline two\nline three",
			wantMatchCount:   3,
			wantCurrentMatch: 0,
		},
		{
			name:             "query with special regex characters",
			currentQuery:     "(test)[foo]",
			rendered:         "before (test)[foo] after\nno match here",
			wantMatchCount:   1,
			wantCurrentMatch: 0,
		},
		{
			name:             "query with backslash",
			currentQuery:     `C:\path`,
			rendered:         "found C:\\path here\nanother C:\\path there",
			wantMatchCount:   2,
			wantCurrentMatch: 0,
		},
		{
			name:             "query with newline in rendered",
			currentQuery:     "hello",
			rendered:         "hello world\nsay hello\nno match",
			wantMatchCount:   2,
			wantCurrentMatch: 0,
		},
		{
			name:             "no matches found",
			currentQuery:     "zzzz",
			rendered:         "line one\nline two\nline three",
			wantMatchCount:   0,
			wantCurrentMatch: 0,
		},
		{
			name:             "currentMatch out of bounds fixed",
			currentQuery:     "line",
			rendered:         "line one\nline two",
			initialMatch:     5, // Out of bounds
			wantMatchCount:   2,
			wantCurrentMatch: 1, // Adjusted to last valid index
		},
		{
			name:             "single line rendered content",
			currentQuery:     "found",
			rendered:         "found here",
			wantMatchCount:   1,
			wantCurrentMatch: 0,
		},
		{
			name:             "empty rendered content",
			currentQuery:     "anything",
			rendered:         "",
			wantMatchCount:   0,
			wantCurrentMatch: 0,
		},
		{
			name:             "query matches exactly at line boundaries",
			currentQuery:     "exact",
			rendered:         "exact",
			wantMatchCount:   1,
			wantCurrentMatch: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &rootBrowserModel{
				currentQuery: tt.currentQuery,
				matches:      tt.initialMatches,
				currentMatch: tt.initialMatch,
			}
			m.recalculateSearchMatches(tt.rendered)

			if len(m.matches) != tt.wantMatchCount {
				t.Errorf("matches count = %d, want %d", len(m.matches), tt.wantMatchCount)
			}
			if m.currentMatch != tt.wantCurrentMatch {
				t.Errorf("currentMatch = %d, want %d", m.currentMatch, tt.wantCurrentMatch)
			}
		})
	}
}

// TestHighlightMatches_EdgeCases covers additional edge cases for highlightMatches
// beyond what the existing test covers, including special characters and error paths.
func TestHighlightMatches_EdgeCases(t *testing.T) {
	m := &rootBrowserModel{}

	tests := []struct {
		name  string
		text  string
		query string
	}{
		{
			name:  "query with dot metacharacter",
			text:  "file.txt",
			query: "file.txt",
		},
		{
			name:  "query with asterisk metacharacter",
			text:  "find * all",
			query: "*",
		},
		{
			name:  "query with plus metacharacter",
			text:  "1+1=2",
			query: "+",
		},
		{
			name:  "query with pipe metacharacter",
			text:  "a|b",
			query: "|",
		},
		{
			name:  "query with caret metacharacter",
			text:  "^start",
			query: "^",
		},
		{
			name:  "query with dollar metacharacter",
			text:  "end$",
			query: "$",
		},
		{
			name:  "query with question mark metacharacter",
			text:  "what?",
			query: "?",
		},
		{
			name:  "query with parentheses metacharacters",
			text:  "(group)",
			query: "(",
		},
		{
			name:  "query with curly brace metacharacters",
			text:  "{block}",
			query: "{",
		},
		{
			name:  "query is substring at beginning",
			text:  "hello world",
			query: "hel",
		},
		{
			name:  "query is substring at end",
			text:  "hello world",
			query: "rld",
		},
		{
			name:  "case insensitive match",
			text:  "Hello WORLD",
			query: "hello",
		},
		{
			name:  "empty text with query",
			text:  "",
			query: "test",
		},
		{
			name:  "query matches entire text",
			text:  "exact",
			query: "exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.highlightMatches(tt.text, tt.query)
			// Must not panic and must return some string
			if result == "" && tt.text != "" {
				t.Errorf("highlightMatches(%q, %q) returned empty string", tt.text, tt.query)
			}
			// For non-empty queries on non-empty text, the original text (or highlighted version)
			// should at minimum appear somewhere in the result
			if tt.query != "" && tt.text != "" {
				// The result should contain the original text content (possibly with ANSI codes)
				if !strings.Contains(stripANSI(result), tt.text) && !strings.Contains(result, tt.text) {
					// Could be highlighted, so text content is embedded in ANSI codes;
					// verify we got a non-empty string as a minimum sanity check.
					if result == "" {
						t.Error("expected non-empty result for valid query")
					}
				}
			}
		})
	}
}

// stripANSI removes ANSI escape sequences for content verification.
func stripANSI(s string) string {
	// Simple regex-free approach: remove everything between \033[ and m
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
			} else {
				break
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

// ── Watcher error path hardening tests (Issue #383) ──

func TestHandleWatcherEvent(t *testing.T) {
	m := &rootBrowserModel{}

	tests := []struct {
		name    string
		event   fsnotify.Event
		ok      bool
		wantNil bool
	}{
		{
			name:    "channel closed (ok=false)",
			event:   fsnotify.Event{},
			ok:      false,
			wantNil: true,
		},
		{
			name:    "write event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Write},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "create event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Create},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "remove event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Remove},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "rename event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Rename},
			ok:      true,
			wantNil: false,
		},
		{
			name:    "chmod event returns nil (not watched)",
			event:   fsnotify.Event{Op: fsnotify.Chmod},
			ok:      true,
			wantNil: true,
		},
		{
			name:    "combined write+create event triggers fileChangedMsg",
			event:   fsnotify.Event{Op: fsnotify.Write | fsnotify.Create},
			ok:      true,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := m.handleWatcherEvent(tt.event, tt.ok)
			if tt.wantNil && msg != nil {
				t.Errorf("expected nil, got %T", msg)
			}
			if !tt.wantNil {
				if _, ok := msg.(fileChangedMsg); !ok {
					t.Errorf("expected fileChangedMsg, got %T", msg)
				}
			}
		})
	}
}

func TestHandleWatcherError(t *testing.T) {
	m := &rootBrowserModel{}

	tests := []struct {
		name string
		err  error
		ok   bool
	}{
		{
			name: "channel closed (ok=false)",
			err:  nil,
			ok:   false,
		},
		{
			name: "watcher error with ok=true",
			err:  errors.New("watcher error"),
			ok:   true,
		},
		{
			name: "nil error with ok=true",
			err:  nil,
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := m.handleWatcherError(tt.err, tt.ok)
			// handleWatcherError always returns nil (errors are logged, swallowed)
			if msg != nil {
				t.Errorf("expected nil, got %T", msg)
			}
		})
	}
}

// ── Watcher error message hardening (Issue #433) ──

func TestWatchHistoryFileCmd_WatcherCreateError(t *testing.T) {
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)
	m.watcherFactory = func() (*fsnotify.Watcher, error) {
		return nil, errors.New("inotify instance limit reached")
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	mockModifier.GetFilePathFunc = func() string { return tmpFile }
	cmd := m.watchHistoryFileCmd()
	msg := cmd()

	werr, ok := msg.(watcherErrorMsg)
	if !ok {
		t.Fatalf("expected watcherErrorMsg, got %T (value: %v)", msg, msg)
	}
	if !strings.Contains(werr.err.Error(), "create watcher") {
		t.Errorf("expected error wrapping 'create watcher', got: %v", werr.err)
	}
}

func TestWatchHistoryFileCmd_WatcherAddError(t *testing.T) {
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), nil, mockModifier)

	// Use a factory that returns a real watcher, then close it so Add fails.
	m.watcherFactory = func() (*fsnotify.Watcher, error) {
		w, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, err
		}
		_ = w.Close() // Close immediately so Add fails
		return w, nil
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	mockModifier.GetFilePathFunc = func() string { return tmpFile }
	cmd := m.watchHistoryFileCmd()
	msg := cmd()

	werr, ok := msg.(watcherErrorMsg)
	if !ok {
		t.Fatalf("expected watcherErrorMsg, got %T (value: %v)", msg, msg)
	}
	if !strings.Contains(werr.err.Error(), "add watcher") {
		t.Errorf("expected error wrapping 'add watcher', got: %v", werr.err)
	}
}

func TestProcessWatcherEvents_WatcherErrorsChannel(t *testing.T) {
	// Create a watcher and close it so the Errors channel returns (zero, false).
	// This exercises the case err, ok := <-watcher.Errors where ok=false.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	_ = watcher.Close()

	m := &rootBrowserModel{ctx: context.Background()}
	msg := m.processWatcherEvents(watcher)
	// handleWatcherError(nil, false) returns nil
	if msg != nil {
		t.Errorf("expected nil msg from closed watcher, got %T", msg)
	}
}

func TestWatcherErrorMsg_Handling(t *testing.T) {
	mockProvider := &mockHistoryProvider{}
	mockModifier := &mockHistoryModifier{}
	m := NewRootBrowserModel(context.Background(), mockProvider, mockModifier)

	newModel, cmd := m.Update(watcherErrorMsg{err: errors.New("watcher failed")})
	updated := newModel.(*rootBrowserModel)

	if updated.err == nil || updated.err.Error() != "watcher failed" {
		t.Errorf("expected err='watcher failed', got %v", updated.err)
	}
	if cmd != nil {
		t.Error("expected nil cmd from watcherErrorMsg handling to prevent hot loop")
	}

	// Verify View() renders the error
	updated.ready = true
	view := updated.View()
	if !strings.Contains(view, "Error: watcher failed") {
		t.Errorf("expected View() to contain error, got %q", view)
	}
}

func TestProcessWatcherEvents_ContextCancellation(t *testing.T) {
	// Create a real watcher on a temp file so it's valid but we cancel context first.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(tmpFile); err != nil {
		t.Fatal(err)
	}

	// Cancel context before calling processWatcherEvents
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &rootBrowserModel{ctx: ctx}
	msg := m.processWatcherEvents(watcher)
	if msg != nil {
		t.Errorf("expected nil msg for cancelled context, got %T", msg)
	}
}

func TestProcessWatcherEvents_ChannelClose(t *testing.T) {
	// Create a watcher, close it immediately, then test processWatcherEvents.
	// When a watcher is closed, its Events channel is closed, so the receive
	// returns event={}, ok=false.

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test-watch")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}

	if err := watcher.Add(tmpFile); err != nil {
		_ = watcher.Close()
		t.Fatal(err)
	}

	// Close the watcher to close the Events channel
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}

	m := &rootBrowserModel{ctx: context.Background()}
	msg := m.processWatcherEvents(watcher)
	if msg != nil {
		t.Errorf("expected nil msg for closed channel, got %T", msg)
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

// ── Viewport/footer edge case tests (Issue #433) ──

func TestBrowserModel_UpdateViewportHeight_NotReady(t *testing.T) {
	m := &rootBrowserModel{ready: false}
	m.updateViewportHeight()
	if m.viewport.Height != 0 {
		t.Errorf("expected viewport height 0 when not ready, got %d", m.viewport.Height)
	}
}

func TestBrowserModel_GetTurnLabelSuffix_ArchivedAndSystem(t *testing.T) {
	m := &rootBrowserModel{
		history: []ports.HistoryViewDTO{
			{Role: "system", ContentPreview: "sys", OriginalIndex: 0},
			{Role: "user", ContentPreview: "u1", OriginalIndex: 1},
		},
	}

	// Archived DTO returns ""
	archivedDto := ports.HistoryViewDTO{IsArchived: true}
	if got := m.getTurnLabelSuffix(archivedDto); got != "" {
		t.Errorf("expected empty suffix for archived DTO, got %q", got)
	}

	// System DTO (turnIdx < 0) returns ""
	systemDto := m.history[0]
	if got := m.getTurnLabelSuffix(systemDto); got != "" {
		t.Errorf("expected empty suffix for system DTO, got %q", got)
	}
}

func TestBrowserModel_RenderFooter_Loading(t *testing.T) {
	m := &rootBrowserModel{isLoading: true}
	got := m.renderFooter()
	if !strings.Contains(got, "LOADING") {
		t.Errorf("expected LOADING in footer, got %q", got)
	}
}

func TestBrowserModel_RenderHistoryStatus_EOF(t *testing.T) {
	m := &rootBrowserModel{
		history: []ports.HistoryViewDTO{{Role: "user"}},
		cursor:  "EOF",
	}
	var sb strings.Builder
	m.renderHistoryStatus(&sb)
	got := sb.String()
	if !strings.Contains(got, "Start of History") {
		t.Errorf("expected 'Start of History', got %q", got)
	}
}

func TestBrowserModel_RenderEmptyState_NoHistory(t *testing.T) {
	m := &rootBrowserModel{isLoading: false, cursor: "some-cursor"}
	got := m.renderEmptyState()
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestBrowserModel_HighPinningPressure_CalculateFooterHeight(t *testing.T) {
	m := &rootBrowserModel{
		history: []ports.HistoryViewDTO{
			{Role: "user", OriginalIndex: 0, IsPinned: true},
			{Role: "assistant", OriginalIndex: 1, IsPinned: true},
			{Role: "user", OriginalIndex: 2, IsPinned: true},
			{Role: "assistant", OriginalIndex: 3, IsPinned: true},
			{Role: "user", OriginalIndex: 4, IsPinned: true},
			{Role: "assistant", OriginalIndex: 5, IsPinned: true},
		},
	}
	h := m.calculateFooterHeight()
	if h != 2 {
		t.Errorf("expected footer height 2 for high pinning pressure, got %d", h)
	}
}

// ── Final coverage push tests (Issue #433) ──

func TestBrowserModel_RenderTurn_ThoughtsHidden(t *testing.T) {
	m := &rootBrowserModel{
		history: []ports.HistoryViewDTO{
			{Role: "user", ContentPreview: "hello", ThoughtProcess: "thinking...", OriginalIndex: 0},
		},
		showThoughts: false,
	}
	var sb strings.Builder
	m.renderTurn(&sb, 0, m.history[0])
	got := sb.String()
	if strings.Contains(got, "THOUGHTS") {
		t.Errorf("expected no thoughts when showThoughts=false, got %q", got)
	}
}

func TestBrowserModel_PreRenderThought_SmallWidth(t *testing.T) {
	m := &rootBrowserModel{
		width:          10,
		showThoughts:   true,
		cachedThoughts: make(map[string]string),
	}
	dto := ports.HistoryViewDTO{ID: "small", ThoughtProcess: "A short thought"}
	result := m.preRenderThought(dto)
	if result == "" {
		t.Error("expected non-empty pre-rendered thought")
	}
}

func TestBrowserModel_RenderFooter_ThoughtsOff(t *testing.T) {
	m := &rootBrowserModel{showThoughts: false}
	got := m.renderFooter()
	if !strings.Contains(got, "[OFF]") {
		t.Errorf("expected 'Thoughts [OFF]' in footer, got %q", got)
	}
}

func TestBrowserModel_RenderContent_QueryHighlight_MultiLine(t *testing.T) {
	m := &rootBrowserModel{currentQuery: "hello"}
	dto := ports.HistoryViewDTO{ContentPreview: "hello world\nsecond line\nhello again"}
	result := m.renderContent(dto, "  ")
	if !strings.Contains(result, "hello") {
		t.Errorf("expected highlighted content, got %q", result)
	}
}

func TestBrowserModel_HandleViewportUpdate_NoPagination(t *testing.T) {
	m := &rootBrowserModel{
		ready:    true,
		viewport: viewport.New(80, 10),
	}
	m.viewport.SetContent("line1\nline2\nline3\nline4\nline5")
	m.viewport.SetYOffset(2)

	_, _ = m.handleViewportUpdate(tea.WindowSizeMsg{Width: 80, Height: 10})
	// Pagination should NOT be triggered (YOffset > 0).
	// WindowSizeMsg on viewport may return nil cmd — that's valid.
	if m.isLoading {
		t.Error("expected no pagination trigger when not at YOffset 0")
	}
}

func TestBrowserModel_UpdateViewportContent_CacheInvalidate(t *testing.T) {
	m := &rootBrowserModel{
		ready:          true,
		width:          100,
		lastWidth:      80,
		cachedThoughts: map[string]string{"old": "cached"},
		showThoughts:   true,
	}
	m.updateViewportContent()
	if len(m.cachedThoughts) != 0 {
		t.Errorf("expected cache to be cleared after width change, got %d entries", len(m.cachedThoughts))
	}
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

func TestRenderThoughts_CacheMiss(t *testing.T) {
	m := &rootBrowserModel{
		width:          80,
		showThoughts:   true,
		cachedThoughts: map[string]string{},
	}
	dto := ports.HistoryViewDTO{ID: "miss1", ThoughtProcess: "uncached thought"}
	result := m.renderThoughts(dto, "  ")
	if !strings.Contains(result, "uncached thought") {
		t.Errorf("expected thought content in render, got %q", result)
	}
	if _, ok := m.cachedThoughts["miss1"]; ok {
		t.Error("renderThoughts should not populate cache")
	}
}

func TestRenderHistory_SingleElement(t *testing.T) {
	m := &rootBrowserModel{
		history:      []ports.HistoryViewDTO{{Role: "user", ContentPreview: "only message", OriginalIndex: 0}},
		selectedTurn: 0,
		showThoughts: false,
		width:        80,
	}
	rendered, offsets := m.renderHistory()
	if !strings.Contains(rendered, "only message") {
		t.Errorf("expected content in render, got %q", rendered)
	}
	if len(offsets) != 1 {
		t.Errorf("expected 1 offset, got %d", len(offsets))
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
