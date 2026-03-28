// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
			msg:        suggestionsMsg([]string{"foo", "bar"}),
			wantIdx:    -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
			defer m.Destroy()
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
			if sm, ok := tt.msg.(suggestionsMsg); ok {
				if len(m.suggester.Suggestions) != len(sm) {
					t.Errorf("got %d suggestions, want %d", len(m.suggester.Suggestions), len(sm))
				}
			}
		})
	}
}

func TestModel_View(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc, 1*time.Millisecond).(*promptModel)
	defer m.Destroy()

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
	m := &promptModel{finalPrompt: "test"}
	if m.FinalPrompt() != "test" {
		t.Errorf("Expected 'test', got %q", m.FinalPrompt())
	}
}

func TestSuggester(t *testing.T) {
	s := &suggester{}
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
	ta := newTextArea()
	ta.SetValue("test")
	if ta.Value() != "test" {
		t.Errorf("expected 'test', got %q", ta.Value())
	}
	if ta.View() == "" {
		t.Error("expected non-empty view")
	}
}

func TestSuggester_Empty(t *testing.T) {
	s := &suggester{}
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
	m := NewModel(svc, 1*time.Millisecond).(*promptModel)
	defer m.Destroy()

	cmd := m.getSuggestions(context.Background(), "")
	msg := cmd()

	suggMsg, ok := msg.(suggestionsMsg)
	if !ok {
		t.Fatalf("Expected suggestionsMsg, got %T", msg)
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

func TestModel_TeaInterface(t *testing.T) {
	// Silence dead_code_graph false positives for tea.Model implementations
	m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
	defer m.Destroy()

	// These methods are called dynamically by Bubble Tea, so we explicitly
	// reference them here to satisfy static analysis AST reference counting.
	_ = m.Init()
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	_ = m.View()
}

func TestModel_Update_TokenAwareAutocomplete(t *testing.T) {
	tests := []struct {
		name         string
		initialInput string
		suggestions  []string
		wantValue    string
	}{
		{
			name:         "replace only last word with file path",
			initialInput: "What is the content of ./f",
			suggestions:  []string{"./foo.txt"},
			wantValue:    "What is the content of ./foo.txt",
		},
		{
			name:         "replace entire line when suggestion contains space",
			initialInput: "How do",
			suggestions:  []string{"How do I reverse a string?"},
			wantValue:    "How do I reverse a string?",
		},
		{
			name:         "replace entire line when input has no space",
			initialInput: "./f",
			suggestions:  []string{"./foo.txt"},
			wantValue:    "./foo.txt",
		},
		{
			name:         "replace only last word even if input has many spaces",
			initialInput: "cat /etc/p",
			suggestions:  []string{"/etc/passwd"},
			wantValue:    "cat /etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
			defer m.Destroy()
			m.input.SetValue(tt.initialInput)
			m.suggester.Suggestions = tt.suggestions
			m.suggester.Index = -1 // No selection initially

			// Trigger Tab
			m.Update(tea.KeyMsg{Type: tea.KeyTab})

			if m.input.Value() != tt.wantValue {
				t.Errorf("got input value %q; want %q", m.input.Value(), tt.wantValue)
			}
		})
	}
}

func TestModel_Update_AdditionalCases(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc, 1*time.Millisecond).(*promptModel)
	defer m.Destroy()

	t.Run("debounce message", func(t *testing.T) {
		m.input.SetValue("test")
		_, cmd := m.Update(debounceMsg{value: "test"})
		if cmd == nil {
			t.Error("expected cmd for debounceMsg")
		}
	})

	t.Run("debounce message value mismatch", func(t *testing.T) {
		m.input.SetValue("new value")
		_, cmd := m.Update(debounceMsg{value: "old value"})
		if cmd != nil {
			t.Error("expected nil cmd for mismatched debounceMsg")
		}
	})

	t.Run("error message", func(t *testing.T) {
		err := fmt.Errorf("test error")
		_, cmd := m.Update(err)
		if cmd != nil {
			t.Error("expected nil cmd for error")
		}
		if !errors.Is(m.err, err) {
			t.Errorf("expected error %v, got %v", err, m.err)
		}
	})

	t.Run("unhandled message", func(t *testing.T) {
		_, cmd := m.Update(struct{ tea.Msg }{Msg: nil})
		if cmd != nil {
			t.Error("expected nil cmd for unhandled message")
		}
	})
}

type integrationMockSuggestionSvc struct {
	suggestion string
}

func (m *integrationMockSuggestionSvc) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return []string{m.suggestion}, nil
}

func (m *integrationMockSuggestionSvc) RecordPrompt(prompt string) error {
	return nil
}

func TestPromptModel_Integration_SuggestionsAreRendered(t *testing.T) {
	mockSvc := &integrationMockSuggestionSvc{
		suggestion: "guaranteed-suggestion-item",
	}

	m := NewModel(mockSvc, 1*time.Millisecond).(*promptModel)
	defer m.Destroy()

	cmd := m.Init()
	// Execute the command to simulate the background fetch completing.
	// Since Init() returns a tea.Batch, we execute the batch and find the suggestions message.
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if res := c(); res != nil {
				if s, ok := res.(suggestionsMsg); ok {
					msg = s
					break
				}
			}
		}
	}

	suggMsg, ok := msg.(suggestionsMsg)
	if !ok {
		t.Fatalf("CRITICAL REGRESSION: The TUI failed to render the suggestion. Expected suggestionsMsg, got %T", msg)
	}

	m.Update(suggMsg)

	output := m.View()
	if !strings.Contains(output, "guaranteed-suggestion-item") {
		t.Errorf("CRITICAL REGRESSION: The TUI failed to render the suggestion.")
	}
}
