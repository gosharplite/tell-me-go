// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel_WithThought(t *testing.T) {
	model := NewModel("hello world", "thinking...")

	if model.EditedText() != "hello world" {
		t.Errorf("expected EditedText %q, got %q", "hello world", model.EditedText())
	}
	if model.EditedThought() != "thinking..." {
		t.Errorf("expected EditedThought %q, got %q", "thinking...", model.EditedThought())
	}
	if model.WasSaved() {
		t.Error("expected WasSaved() to be false for new model")
	}
	if model.WasAborted() {
		t.Error("expected WasAborted() to be false for new model")
	}
	if model.focused != 0 {
		t.Errorf("expected focused 0 (text area), got %d", model.focused)
	}
	if !model.textArea.Focused() {
		t.Error("expected text area to be focused")
	}
}

func TestNewModel_EmptyThought(t *testing.T) {
	model := NewModel("hello", "")

	if model.EditedText() != "hello" {
		t.Errorf("expected EditedText %q, got %q", "hello", model.EditedText())
	}
	if model.EditedThought() != "" {
		t.Errorf("expected empty EditedThought, got %q", model.EditedThought())
	}
	// Should not panic — constructed successfully
}

func TestTabSwitchesFocus(t *testing.T) {
	model := NewModel("text", "thought")

	// Start: text area focused (0)
	if model.focused != 0 {
		t.Errorf("expected focused 0, got %d", model.focused)
	}
	if !model.textArea.Focused() {
		t.Error("expected text area focused initially")
	}

	// Tab: switch to thought (1)
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	editor := newModel.(*EditorModel)
	if editor.focused != 1 {
		t.Errorf("expected focused 1 after Tab, got %d", editor.focused)
	}
	if !editor.thoughtArea.Focused() {
		t.Error("expected thought area focused after Tab")
	}
	if editor.textArea.Focused() {
		t.Error("expected text area blurred after Tab")
	}

	// Tab again: switch back to text (0)
	newModel, _ = editor.Update(tea.KeyMsg{Type: tea.KeyTab})
	editor = newModel.(*EditorModel)
	if editor.focused != 0 {
		t.Errorf("expected focused 0 after second Tab, got %d", editor.focused)
	}
	if !editor.textArea.Focused() {
		t.Error("expected text area focused after second Tab")
	}
}

func TestShiftTabSwitchesFocus(t *testing.T) {
	model := NewModel("text", "thought")

	// Shift+Tab from text (0) goes to thought (1)
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	editor := newModel.(*EditorModel)
	if editor.focused != 1 {
		t.Errorf("expected focused 1 after Shift+Tab, got %d", editor.focused)
	}
	if !editor.thoughtArea.Focused() {
		t.Error("expected thought area focused after Shift+Tab")
	}
}

func TestCtrlS_SavesAndQuits(t *testing.T) {
	model := NewModel("hello", "thinking...")
	model.ready = true
	model.width = 80
	model.height = 24

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	editor := newModel.(*EditorModel)
	if !editor.WasSaved() {
		t.Error("expected WasSaved() to be true after Ctrl+S")
	}
	if editor.WasAborted() {
		t.Error("expected WasAborted() to be false after Ctrl+S")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command after Ctrl+S")
	}
}

func TestEsc_AbortsAndQuits(t *testing.T) {
	model := NewModel("hello", "thinking...")
	model.ready = true
	model.width = 80
	model.height = 24

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	editor := newModel.(*EditorModel)
	if !editor.WasAborted() {
		t.Error("expected WasAborted() to be true after Esc")
	}
	if editor.WasSaved() {
		t.Error("expected WasSaved() to be false after Esc")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command after Esc")
	}
}

func TestQ_TypesCharacter(t *testing.T) {
	model := NewModel("hello", "thinking...")
	model.ready = true
	model.width = 80
	model.height = 24

	// Pressing "q" should type the character, not abort
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	editor := newModel.(*EditorModel)
	if editor.WasAborted() {
		t.Error("pressing 'q' should not abort the editor")
	}
	// Verify "q" was appended to the text area value
	if !strings.Contains(editor.textArea.Value(), "q") {
		t.Error("expected 'q' to be typed into the text area")
	}
}

func TestCtrlC_AbortsAndQuits(t *testing.T) {
	model := NewModel("hello", "thinking...")
	model.ready = true
	model.width = 80
	model.height = 24

	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	editor := newModel.(*EditorModel)
	if !editor.WasAborted() {
		t.Error("expected WasAborted() to be true after Ctrl+C")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command after Ctrl+C")
	}
}

func TestEditedText_TrimsWhitespace(t *testing.T) {
	model := NewModel("  hello world  \n\n", "")
	if model.EditedText() != "hello world" {
		t.Errorf("expected trimmed text %q, got %q", "hello world", model.EditedText())
	}
}

func TestEditedThought_TrimsWhitespace(t *testing.T) {
	model := NewModel("text", "\n  thinking content  \n")
	if model.EditedThought() != "thinking content" {
		t.Errorf("expected trimmed thought %q, got %q", "thinking content", model.EditedThought())
	}
}

func TestWindowSizeMsg_Layout(t *testing.T) {
	model := NewModel("hello", "thought")
	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	editor := newModel.(*EditorModel)
	if !editor.ready {
		t.Error("expected ready to be true after WindowSizeMsg")
	}
	if editor.width != 80 {
		t.Errorf("expected width 80, got %d", editor.width)
	}
	if editor.height != 30 {
		t.Errorf("expected height 30, got %d", editor.height)
	}
}

func TestWindowSizeMsg_ClampsMinimum(t *testing.T) {
	model := NewModel("hello", "thought")
	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 5, Height: 3})

	editor := newModel.(*EditorModel)
	if editor.width != 20 {
		t.Errorf("expected clamped width 20, got %d", editor.width)
	}
	if editor.height != 10 {
		t.Errorf("expected clamped height 10, got %d", editor.height)
	}
}

func TestView_BeforeReady(t *testing.T) {
	model := NewModel("hello", "thought")
	view := model.View()
	if view != "Initializing editor..." {
		t.Errorf("expected initializing message, got %q", view)
	}
}

func TestView_AfterReady(t *testing.T) {
	model := NewModel("hello", "thought")
	model.ready = true
	model.width = 80
	model.height = 24
	// Set up viewports
	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	editor := newModel.(*EditorModel)

	view := editor.View()
	if view == "Initializing editor..." {
		t.Error("expected rendered view, got initializing message")
	}
}

func TestTextTyping_UpdatesTextArea(t *testing.T) {
	model := NewModel("initial", "")
	model.ready = true
	model.width = 80
	model.height = 24
	// Set initial state via window size
	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	editor := newModel.(*EditorModel)

	// Type some text
	newModel, _ = editor.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("extra")})
	editor = newModel.(*EditorModel)
	// The text area should have the runes added
	if editor.textArea.Value() == "initial" {
		t.Error("expected text area to have updated value after typing")
	}
}

func TestThoughtTyping_UpdatesThoughtArea(t *testing.T) {
	model := NewModel("text", "initial thought")
	model.ready = true
	model.width = 80
	model.height = 24
	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	editor := newModel.(*EditorModel)

	// Switch to thought area
	newModel, _ = editor.Update(tea.KeyMsg{Type: tea.KeyTab})
	editor = newModel.(*EditorModel)
	// Type something
	newModel, _ = editor.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("extra")})
	editor = newModel.(*EditorModel)

	if editor.thoughtArea.Value() == "initial thought" {
		t.Error("expected thought area to have updated value after typing")
	}
}

func TestInit_ReturnsBlinkCmd(t *testing.T) {
	model := NewModel("text", "thought")
	cmd := model.Init()
	if cmd == nil {
		t.Error("expected Init to return a non-nil command")
	}
}

func TestNonKeyMsg_ForwardedToFocusedTextArea(t *testing.T) {
	model := NewModel("hello", "thought")
	model.ready = true
	model.width = 80
	model.height = 24
	newModel, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	editor := newModel.(*EditorModel)

	// Send a non-key, non-window message — should be forwarded without panic
	newModel, _ = editor.Update(struct{}{})
	_ = newModel.(*EditorModel)
	// Should not panic
}
