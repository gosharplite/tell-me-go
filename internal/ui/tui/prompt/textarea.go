// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	textareaStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true).
		BorderForeground(lipgloss.Color("240"))
)

// TextArea wraps the Bubble Tea textarea component with fixed height.
type TextArea struct {
	Model textarea.Model
}

// NewTextArea creates a new TextArea with default styling and fixed height.
func NewTextArea() TextArea {
	ta := textarea.New()
	ta.Placeholder = "Type your message here... (Alt+Enter or Ctrl+S to submit, Esc to abort)"
	ta.Focus()
	ta.SetHeight(10) // Fixed height constraint to prevent overflow
	ta.SetWidth(80)  // Default width, can be updated on resize
	ta.ShowLineNumbers = false
	return TextArea{Model: ta}
}

// Update handles standard textarea messages.
func (t *TextArea) Update(msg tea.Msg) (TextArea, tea.Cmd) {
	var cmd tea.Cmd
	t.Model, cmd = t.Model.Update(msg)
	return *t, cmd
}

// View renders the textarea component.
func (t TextArea) View() string {
	return textareaStyle.Render(t.Model.View())
}

// SetValue sets the content of the textarea.
func (t *TextArea) SetValue(v string) {
	t.Model.SetValue(v)
}

// Value returns the current content of the textarea.
func (t TextArea) Value() string {
	return t.Model.Value()
}
