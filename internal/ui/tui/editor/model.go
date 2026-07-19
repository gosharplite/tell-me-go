// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package editor provides a Bubble Tea model for interactive editing
// of a model turn's text and thinking content.
package editor

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EditorModel is a tea.Model for editing a model turn's text and thought content.
// It presents two scrollable textareas:
//   - Top area: model response text
//   - Bottom area: thinking/reasoning content
//
// Key bindings:
//
//	Tab / Shift+Tab     — switch focus between textareas
//	Ctrl+S              — save both and quit (sets saved=true)
//	Esc / Ctrl+C / q    — abort and quit (sets saved=false)
type EditorModel struct {
	textArea    textarea.Model
	thoughtArea textarea.Model
	textVP      viewport.Model
	thoughtVP   viewport.Model

	focused int // 0 = text, 1 = thought

	width  int
	height int
	ready  bool

	saved   bool
	aborted bool
}

// NewModel creates a new EditorModel pre-populated with the given text and thought.
// If thought is empty, the thought textarea shows a placeholder.
func NewModel(text, thought string) *EditorModel {
	ta := textarea.New()
	ta.SetValue(text)
	ta.Placeholder = "Model response text..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.Focus()

	tha := textarea.New()
	tha.SetValue(thought)
	tha.Placeholder = "(no thinking content)"
	tha.ShowLineNumbers = false
	tha.CharLimit = 0

	return &EditorModel{
		textArea:    ta,
		thoughtArea: tha,
		focused:     0,
	}
}

// EditedText returns the current text in the main text area.
func (m *EditorModel) EditedText() string {
	return strings.TrimSpace(m.textArea.Value())
}

// EditedThought returns the current text in the thought area.
func (m *EditorModel) EditedThought() string {
	return strings.TrimSpace(m.thoughtArea.Value())
}

// WasSaved returns true if the user pressed Ctrl+S to save.
func (m *EditorModel) WasSaved() bool {
	return m.saved
}

// WasAborted returns true if the user pressed Esc/q/Ctrl+C.
func (m *EditorModel) WasAborted() bool {
	return m.aborted
}

// Init returns the initial command for the model.
func (m *EditorModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles incoming messages and updates the model state.
func (m *EditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	// Forward to focused textarea
	var cmd tea.Cmd
	if m.focused == 0 {
		m.textArea, cmd = m.textArea.Update(msg)
	} else {
		m.thoughtArea, cmd = m.thoughtArea.Update(msg)
	}
	return m, cmd
}

func (m *EditorModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		m.saved = true
		return m, tea.Quit

	case "esc", "ctrl+c", "q":
		m.aborted = true
		return m, tea.Quit

	case "tab":
		m.focused = (m.focused + 1) % 2
		m.syncFocus()
		return m, nil

	case "shift+tab":
		// Reverse: go from 1→0 or 0→1 (same as forward with 2 elements)
		m.focused = (m.focused + 1) % 2
		m.syncFocus()
		return m, nil
	}

	// Forward to focused textarea
	var cmd tea.Cmd
	if m.focused == 0 {
		m.textArea, cmd = m.textArea.Update(msg)
	} else {
		m.thoughtArea, cmd = m.thoughtArea.Update(msg)
	}
	return m, cmd
}

func (m *EditorModel) syncFocus() {
	if m.focused == 0 {
		m.textArea.Focus()
		m.thoughtArea.Blur()
	} else {
		m.textArea.Blur()
		m.thoughtArea.Focus()
	}
}

func (m *EditorModel) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height

	if m.width < 20 {
		m.width = 20
	}
	if m.height < 10 {
		m.height = 10
	}

	// Divide: top 55% for text, bottom 45% for thought (minus label lines)
	labelHeight := 1                                              // one line for each label
	dividerHeight := 1                                            // divider line
	available := m.height - (2 * labelHeight) - dividerHeight - 1 // -1 for footer
	textHeight := available * 55 / 100
	thoughtHeight := available - textHeight

	if textHeight < 3 {
		textHeight = 3
	}
	if thoughtHeight < 3 {
		thoughtHeight = 3
	}

	m.textArea.SetWidth(m.width - 4)
	m.textArea.SetHeight(textHeight)
	m.thoughtArea.SetWidth(m.width - 4)
	m.thoughtArea.SetHeight(thoughtHeight)

	if !m.ready {
		m.textVP = viewport.New(m.width-2, textHeight)
		m.thoughtVP = viewport.New(m.width-2, thoughtHeight)
		m.ready = true
	} else {
		m.textVP.Width = m.width - 2
		m.textVP.Height = textHeight
		m.thoughtVP.Width = m.width - 2
		m.thoughtVP.Height = thoughtHeight
	}

	m.textVP.SetContent(m.textArea.View())
	m.thoughtVP.SetContent(m.thoughtArea.View())

	return m, nil
}

var (
	labelStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Padding(0, 1)
	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Padding(0, 1)
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Padding(0, 1)
)

// View renders the current state of the model.
func (m *EditorModel) View() string {
	if !m.ready {
		return "Initializing editor..."
	}

	// Update viewport content from textareas
	m.textVP.SetContent(m.textArea.View())
	m.thoughtVP.SetContent(m.thoughtArea.View())

	var sb strings.Builder

	// Text label
	sb.WriteString(labelStyle.Render("📝 Model Response"))
	if m.focused == 0 {
		sb.WriteString(" ◀")
	}
	sb.WriteString("\n")
	sb.WriteString(m.textVP.View())
	sb.WriteString("\n")

	// Divider
	sb.WriteString(dividerStyle.Render(strings.Repeat("─", m.width-2)))
	sb.WriteString("\n")

	// Thought label
	sb.WriteString(labelStyle.Render("💭 Thinking"))
	if m.focused == 1 {
		sb.WriteString(" ◀")
	}
	sb.WriteString("\n")
	sb.WriteString(m.thoughtVP.View())
	sb.WriteString("\n")

	// Footer
	sb.WriteString(footerStyle.Render("Ctrl+S: save & exit  |  Tab: switch  |  Esc/q: abort"))

	return sb.String()
}
