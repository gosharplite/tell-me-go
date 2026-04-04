// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type mockSuggestionSvc struct{}

func (m *mockSuggestionSvc) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return []string{"suggested"}, nil
}

func (m *mockSuggestionSvc) RecordPrompt(ctx context.Context, prompt string) error {
	return nil
}

func (m *mockSuggestionSvc) Close(ctx context.Context) error {
	return nil
}

// Test helpers to keep cyclomatic complexity low in TestModel_Update.
func assertSubmitted(t *testing.T, m PromptModel, want bool) {
	t.Helper()
	pm := m.(*promptModel)
	if pm.submitted != want {
		t.Errorf("submitted = %v, want %v", pm.submitted, want)
	}
}

func assertAborted(t *testing.T, m PromptModel, want bool) {
	t.Helper()
	if m.Aborted() != want {
		t.Errorf("aborted = %v, want %v", m.Aborted(), want)
	}
}

func assertValue(t *testing.T, m PromptModel, want string) {
	t.Helper()
	pm := m.(*promptModel)
	if pm.input.Value() != want {
		t.Errorf("input value = %q, want %q", pm.input.Value(), want)
	}
}

func assertIndex(t *testing.T, m PromptModel, want int) {
	t.Helper()
	pm := m.(*promptModel)
	if pm.suggester.Index != want {
		t.Errorf("suggester index = %d, want %d", pm.suggester.Index, want)
	}
}

func assertFinalPrompt(t *testing.T, m PromptModel, want string) {
	t.Helper()
	if m.FinalPrompt() != want {
		t.Errorf("finalPrompt = %q, want %q", m.FinalPrompt(), want)
	}
}

func TestModel_Update(t *testing.T) {
	tests := []struct {
		name        string
		setupModel  func() PromptModel
		msg         tea.Msg
		assertState func(*testing.T, PromptModel)
		wantCmd     bool
	}{
		{
			name: "empty_input_alt_enter_ignored",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("   ")
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyEnter, Alt: true},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertSubmitted(t, m, false)
			},
			wantCmd: false,
		},
		{
			name: "empty_input_ctrl_s_ignored",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("   ")
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlS},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertSubmitted(t, m, false)
			},
			wantCmd: false,
		},
		{
			name: "valid_input_ctrl_s_submits",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("  hello  ")
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlS},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertSubmitted(t, m, true)
				assertFinalPrompt(t, m, "hello")
			},
			wantCmd: true,
		},
		{
			name: "abort_with_esc",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg: tea.KeyMsg{Type: tea.KeyEsc},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertAborted(t, m, true)
			},
			wantCmd: true,
		},
		{
			name: "abort_with_ctrl_c",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg: tea.KeyMsg{Type: tea.KeyCtrlC},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertAborted(t, m, true)
			},
			wantCmd: true,
		},
		{
			name: "tab_cycles_suggestions_forward",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.suggester.Suggestions = []string{"foo", "bar"}
				m.suggester.Index = -1
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertIndex(t, m, 0)
				assertValue(t, m, "foo")
			},
			wantCmd: false,
		},
		{
			name: "tab_cycles_suggestions_forward_twice",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.suggester.Suggestions = []string{"foo", "bar"}
				m.suggester.Index = 0
				m.input.SetValue("foo")
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertIndex(t, m, 1)
				assertValue(t, m, "bar")
			},
			wantCmd: false,
		},
		{
			name: "shift_tab_cycles_suggestions_backward",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.suggester.Suggestions = []string{"foo", "bar"}
				m.suggester.Index = 1
				m.input.SetValue("bar")
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyShiftTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertIndex(t, m, 0)
				assertValue(t, m, "foo")
			},
			wantCmd: false,
		},
		{
			name: "input_change_triggers_debounce",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertValue(t, m, "a")
			},
			wantCmd: true,
		},
		{
			name: "window_resize_updates_input_width",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg: tea.WindowSizeMsg{Width: 100, Height: 50},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				pm := m.(*promptModel)
				// Underlying textarea accounts for internal padding/borders.
				// 100 (msg) - 4 (fixed offset) - 2 (internal padding) = 94.
				if pm.input.Model.Width() != 94 {
					t.Errorf("expected width 94, got %d", pm.input.Model.Width())
				}
			},
			wantCmd: false,
		},
		{
			name: "suggestions_msg_updates_suggester",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg: suggestionsMsg([]string{"foo", "bar"}),
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				pm := m.(*promptModel)
				if len(pm.suggester.Suggestions) != 2 {
					t.Errorf("expected 2 suggestions, got %d", len(pm.suggester.Suggestions))
				}
			},
			wantCmd: false,
		},
		{
			name: "debounce_msg_triggers_fetch",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("test")
				return m
			},
			msg:     debounceMsg{value: "test"},
			wantCmd: true,
		},
		{
			name: "debounce_msg_value_mismatch_ignored",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("new value")
				return m
			},
			msg:     debounceMsg{value: "old value"},
			wantCmd: false,
		},
		{
			name: "error_msg_sets_error_state",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg: errors.New("test error"),
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				pm := m.(*promptModel)
				if pm.err == nil || pm.err.Error() != "test error" {
					t.Errorf("expected error 'test error', got %v", pm.err)
				}
			},
			wantCmd: false,
		},
		{
			name: "unhandled_message_returns_model_as_is",
			setupModel: func() PromptModel {
				return NewModel(&mockSuggestionSvc{}, 1*time.Millisecond)
			},
			msg:     struct{ tea.Msg }{Msg: nil},
			wantCmd: false,
		},
		{
			name: "token_aware_replace_last_word_file_path",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("What is the content of ./f")
				m.suggester.Suggestions = []string{"./foo.txt"}
				m.suggester.Index = -1
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertValue(t, m, "What is the content of ./foo.txt")
			},
			wantCmd: false,
		},
		{
			name: "token_aware_replace_entire_line_suggestion_with_space",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("How do")
				m.suggester.Suggestions = []string{"How do I reverse a string?"}
				m.suggester.Index = -1
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertValue(t, m, "How do I reverse a string?")
			},
			wantCmd: false,
		},
		{
			name: "token_aware_replace_entire_line_no_space_in_input",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("./f")
				m.suggester.Suggestions = []string{"./foo.txt"}
				m.suggester.Index = -1
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertValue(t, m, "./foo.txt")
			},
			wantCmd: false,
		},
		{
			name: "token_aware_replace_last_word_many_spaces",
			setupModel: func() PromptModel {
				m := NewModel(&mockSuggestionSvc{}, 1*time.Millisecond).(*promptModel)
				m.input.SetValue("cat /etc/p")
				m.suggester.Suggestions = []string{"/etc/passwd"}
				m.suggester.Index = -1
				return m
			},
			msg: tea.KeyMsg{Type: tea.KeyTab},
			assertState: func(t *testing.T, m PromptModel) {
				t.Helper()
				assertValue(t, m, "cat /etc/passwd")
			},
			wantCmd: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := tt.setupModel()
			defer m.Destroy()

			newModel, cmd := m.Update(tt.msg)
			updatedModel := newModel.(PromptModel)

			if (cmd != nil) != tt.wantCmd {
				t.Errorf("got cmd presence %v, want %v", cmd != nil, tt.wantCmd)
			}

			if tt.assertState != nil {
				tt.assertState(t, updatedModel)
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

func (m *mockSuggestionSvcMultiLine) RecordPrompt(ctx context.Context, prompt string) error {
	return nil
}

func (m *mockSuggestionSvcMultiLine) Close(ctx context.Context) error {
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

type integrationMockSuggestionSvc struct {
	suggestion string
}

func (m *integrationMockSuggestionSvc) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return []string{m.suggestion}, nil
}

func (m *integrationMockSuggestionSvc) RecordPrompt(ctx context.Context, prompt string) error {
	return nil
}

func (m *integrationMockSuggestionSvc) Close(ctx context.Context) error {
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
