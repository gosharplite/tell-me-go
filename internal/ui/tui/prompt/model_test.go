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

func TestModel_Update_SubmitEmpty(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	// Ensure input is empty or whitespace
	m.input.SetValue("   ")

	// Simulate Alt+Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Errorf("Expected nil command for empty submission, got %v", cmd)
	}
	if m.submitted {
		t.Error("Expected submitted to be false for empty submission")
	}

	// Simulate Ctrl+S
	msg = tea.KeyMsg{Type: tea.KeyCtrlS}
	_, cmd = m.Update(msg)

	if cmd != nil {
		t.Errorf("Expected nil command for empty submission via Ctrl+S, got %v", cmd)
	}
	if m.submitted {
		t.Error("Expected submitted to be false for empty submission via Ctrl+S")
	}
}

func TestModel_Update_SubmitNonEmpty(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	m.input.SetValue("  hello  ")

	msg := tea.KeyMsg{Type: tea.KeyCtrlS}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("Expected non-nil command for valid submission")
	}
	if !m.submitted {
		t.Error("Expected submitted to be true for valid submission")
	}
	if m.finalPrompt != "hello" {
		t.Errorf("Expected finalPrompt to be 'hello', got %q", m.finalPrompt)
	}
}

func TestModel_Update_AbortEsc(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Error("Expected non-nil command for Esc")
	}
	if !m.Aborted() {
		t.Error("Expected aborted to be true")
	}
}

func TestModel_Update_AbortCtrlC(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Error("Expected non-nil command for Ctrl+C")
	}
	if !m.Aborted() {
		t.Error("Expected aborted to be true")
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

func TestModel_Update_SuggestionsMsg(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	msg := SuggestionsMsg([]string{"foo", "bar"})
	m.Update(msg)

	if len(m.suggester.Suggestions) != 2 {
		t.Errorf("Expected 2 suggestions, got %d", len(m.suggester.Suggestions))
	}
}

func TestModel_Update_WindowSize(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	m.Update(msg)

	// In Bubbles TextArea, setting width subtracts some for padding if specified.
	// But actually, we just want to ensure it's called and something changes.
	if m.input.Model.Width() == 80 { // Default is 80
		t.Error("Expected width to change")
	}
}

func TestModel_Update_Tab(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)
	m.suggester.Suggestions = []string{"foo", "bar"}

	msg := tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)

	if m.suggester.Index != 0 {
		t.Errorf("Expected index 0, got %d", m.suggester.Index)
	}
	if m.input.Value() != "foo" {
		t.Errorf("Expected 'foo', got %q", m.input.Value())
	}

	msg = tea.KeyMsg{Type: tea.KeyTab}
	m.Update(msg)
	if m.suggester.Index != 1 {
		t.Errorf("Expected index 1, got %d", m.suggester.Index)
	}
	if m.input.Value() != "bar" {
		t.Errorf("Expected 'bar', got %q", m.input.Value())
	}

	msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	m.Update(msg)
	if m.suggester.Index != 0 {
		t.Errorf("Expected index 0, got %d", m.suggester.Index)
	}
}

func TestModel_Update_InputChanged(t *testing.T) {
	svc := &mockSuggestionSvc{}
	m := NewModel(svc)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Error("Expected non-nil command for input change (debounce)")
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
