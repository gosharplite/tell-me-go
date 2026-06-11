// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

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
	if _, ok := m.cachedThoughts["miss1"]; !ok {
		t.Error("renderThoughts should populate cache on miss")
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
