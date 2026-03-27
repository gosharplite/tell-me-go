// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type mockSuggestionSvc struct{}

func (m *mockSuggestionSvc) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return []string{"suggested"}, nil
}

func (m *mockSuggestionSvc) RecordPrompt(prompt string) error {
	return nil
}

func TestModel_Update(t *testing.T) {
	tests := []struct {
		name          string
		initialInput  string
		initialSuggs  []string
		initialIdx    int
		msg           tea.Msg
		wantSubmitted bool
		wantAborted   bool
		wantCmd       bool
		wantFinal     string
		wantIdx       int
	}{
		{
			name:          "Empty input Alt+Enter",
			initialInput:  "   ",
			initialIdx:    -1,
			msg:           tea.KeyMsg{Type: tea.KeyEnter, Alt: true},
			wantSubmitted: false,
			wantCmd:       false,
			wantIdx:       -1,
		},
		{
			name:          "Empty input Ctrl+S",
			initialInput:  "   ",
			initialIdx:    -1,
			msg:           tea.KeyMsg{Type: tea.KeyCtrlS},
			wantSubmitted: false,
			wantCmd:       false,
			wantIdx:       -1,
		},
		{
			name:          "Valid input Ctrl+S",
			initialInput:  "  hello  ",
			initialIdx:    -1,
			msg:           tea.KeyMsg{Type: tea.KeyCtrlS},
			wantSubmitted: true,
			wantCmd:       true,
			wantFinal:     "hello",
			wantIdx:       -1,
		},
		{
			name:        "Abort Esc",
			initialIdx:  -1,
			msg:         tea.KeyMsg{Type: tea.KeyEsc},
			wantAborted: true,
			wantCmd:     true,
			wantIdx:     -1,
		},
		{
			name:        "Abort Ctrl+C",
			initialIdx:  -1,
			msg:         tea.KeyMsg{Type: tea.KeyCtrlC},
			wantAborted: true,
			wantCmd:     true,
			wantIdx:     -1,
		},
		{
			name:         "Tab cycle 1",
			initialIdx:   -1,
			initialSuggs: []string{"foo", "bar"},
			msg:          tea.KeyMsg{Type: tea.KeyTab},
			wantFinal:    "foo",
			wantIdx:      0,
		},
		{
			name:         "Tab cycle 2",
			initialInput: "foo",
			initialIdx:   0,
			initialSuggs: []string{"foo", "bar"},
			msg:          tea.KeyMsg{Type: tea.KeyTab},
			wantFinal:    "bar",
			wantIdx:      1,
		},
		{
			name:         "ShiftTab cycle",
			initialInput: "bar",
			initialIdx:   1,
			initialSuggs: []string{"foo", "bar"},
			msg:          tea.KeyMsg{Type: tea.KeyShiftTab},
			wantFinal:    "foo",
			wantIdx:      0,
		},
		{
			name:       "Input changed (debounce)",
			initialIdx: -1,
			msg:        tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}},
			wantCmd:    true,
			wantFinal:  "a",
			wantIdx:    -1,
		},
		{
			name:       "Window resize",
			initialIdx: -1,
			msg:        tea.WindowSizeMsg{Width: 100, Height: 50},
			wantIdx:    -1,
		},
		{
			name:       "Suggestions message",
			initialIdx: -1,
			msg:        SuggestionsMsg([]string{"foo", "bar"}),
			wantIdx:    -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(&mockSuggestionSvc{})
			if tt.initialInput != "" {
				m.input.SetValue(tt.initialInput)
			}
			if len(tt.initialSuggs) > 0 {
				m.suggester.Suggestions = tt.initialSuggs
			}
			m.suggester.Index = tt.initialIdx

			_, cmd := m.Update(tt.msg)

			if (cmd != nil) != tt.wantCmd {
				t.Errorf("got cmd presence %v, want %v", cmd != nil, tt.wantCmd)
			}
			if m.submitted != tt.wantSubmitted {
				t.Errorf("got submitted %v, want %v", m.submitted, tt.wantSubmitted)
			}
			if m.Aborted() != tt.wantAborted {
				t.Errorf("got aborted %v, want %v", m.Aborted(), tt.wantAborted)
			}
			if tt.wantSubmitted && m.FinalPrompt() != tt.wantFinal {
				t.Errorf("got finalPrompt %q, want %q", m.FinalPrompt(), tt.wantFinal)
			}
			if !tt.wantSubmitted && tt.wantFinal != "" && m.input.Value() != tt.wantFinal {
				t.Errorf("got input value %q, want %q", m.input.Value(), tt.wantFinal)
			}
			if m.suggester.Index != tt.wantIdx {
				t.Errorf("got suggester index %d, want %d", m.suggester.Index, tt.wantIdx)
			}
			if ws, ok := tt.msg.(tea.WindowSizeMsg); ok {
				if m.input.Model.Width() == 80 {
					t.Errorf("expected width to change from default 80 for WindowSizeMsg %v", ws)
				}
			}
			if sm, ok := tt.msg.(SuggestionsMsg); ok {
				if len(m.suggester.Suggestions) != len(sm) {
					t.Errorf("got %d suggestions, want %d", len(m.suggester.Suggestions), len(sm))
				}
			}
		})
	}
}

func TestModel_View(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	view := m.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}

	m.submitted = true
	if m.View() != "" {
		t.Error("Expected empty view after submission")
	}

	m.submitted = false
	m.aborted = true
	if m.View() != "" {
		t.Error("Expected empty view after abortion")
	}
}

func TestModel_FinalPrompt(t *testing.T) {
	m := &Model{finalPrompt: "test"}
	if m.FinalPrompt() != "test" {
		t.Errorf("Expected 'test', got %q", m.FinalPrompt())
	}
}

func TestSuggester(t *testing.T) {
	s := &Suggester{}
	s.Update([]string{"a", "b"}, -1)
	if len(s.Suggestions) != 2 {
		t.Error("expected 2 suggestions")
	}

	s.Next()
	if s.Index != 0 {
		t.Errorf("expected index 0, got %d", s.Index)
	}

	s.Next()
	if s.Index != 1 {
		t.Errorf("expected index 1, got %d", s.Index)
	}

	s.Next()
	if s.Index != 0 {
		t.Errorf("expected index 0, got %d", s.Index)
	}

	s.Prev()
	if s.Index != 1 {
		t.Errorf("expected index 1, got %d", s.Index)
	}

	if s.GetSelected() != "b" {
		t.Errorf("expected 'b', got %q", s.GetSelected())
	}

	if s.View() == "" {
		t.Error("expected non-empty view")
	}
}

func TestTextArea(t *testing.T) {
	ta := NewTextArea()
	ta.SetValue("test")
	if ta.Value() != "test" {
		t.Errorf("expected 'test', got %q", ta.Value())
	}
	if ta.View() == "" {
		t.Error("expected non-empty view")
	}
}

func TestSuggester_Empty(t *testing.T) {
	s := &Suggester{}
	s.Next()
	s.Prev()
	if s.GetSelected() != "" {
		t.Error("expected empty")
	}
	if s.View() != "" {
		t.Error("expected empty view")
	}
}

func TestModel_GetSuggestions_FilterLines(t *testing.T) {
	svc := &mockSuggestionSvcMultiLine{}
	m := NewModel(svc)

	cmd := m.getSuggestions(context.Background(), "")
	msg := cmd()

	suggMsg, ok := msg.(SuggestionsMsg)
	if !ok {
		t.Fatalf("Expected SuggestionsMsg, got %T", msg)
	}

	if len(suggMsg) != 2 {
		t.Errorf("Expected 2 suggestions (filtered out the >3 lines one), got %d", len(suggMsg))
	}
	if suggMsg[0] != "one line" || suggMsg[1] != "two\nlines" {
		t.Errorf("Unexpected filtered suggestions: %v", suggMsg)
	}
}

type mockSuggestionSvcMultiLine struct{}

func (m *mockSuggestionSvcMultiLine) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return []string{
		"one line",
		"two\nlines",
		"four\nlines\nreally\nlong", // This should be filtered out
	}, nil
}

func (m *mockSuggestionSvcMultiLine) RecordPrompt(prompt string) error {
	return nil
}
