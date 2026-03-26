// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

var (
	modelStyle = lipgloss.NewStyle().Padding(1, 1)
)

// SuggestionsMsg contains a list of suggested prompts returned from an asynchronous call.
type SuggestionsMsg []string

// debounceMsg is sent after a short delay to trigger suggestion fetching.
type debounceMsg struct {
	value string
}

// Model is the main orchestrator for the TUI prompt.
type Model struct {
	input     TextArea
	suggester Suggester

	// DI
	suggestionSvc ports.SuggestionService

	// State
	finalPrompt string
	ctx         context.Context
	cancel      context.CancelFunc
	err         error
	submitted   bool
	aborted     bool
}

// NewModel creates a new Model with the given suggestion service.
func NewModel(svc ports.SuggestionService) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		input:         NewTextArea(),
		suggester:     Suggester{Index: -1}, // -1 means no suggestion is currently selected/highlighted
		suggestionSvc: svc,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Init initializes the TUI prompt and triggers initial suggestions.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.getSuggestions(""), // Load initial top prompts
	)
}

// Update handles UI interactions and asynchronous messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			m.aborted = true
			return m, tea.Quit

		case tea.KeyCtrlC:
			m.aborted = true
			return m, tea.Quit

		case tea.KeyEnter:
			// Alt+Enter or Ctrl+Enter is typically handled by terminal,
			// here we treat standard Enter as a newline unless it's a specific key combo.
			// But the instruction says Alt+Enter or Ctrl+S to submit.
			// TextArea handles Enter as newline by default.
			if msg.Alt { // Alt+Enter
				m.submit()
				return m, tea.Quit
			}

		case tea.KeyCtrlS:
			m.submit()
			return m, tea.Quit

		case tea.KeyTab, tea.KeyShiftTab:
			if len(m.suggester.Suggestions) > 0 {
				if msg.Type == tea.KeyTab {
					m.suggester.Next()
				} else {
					m.suggester.Prev()
				}
				selected := m.suggester.GetSelected()
				if selected != "" {
					m.input.SetValue(selected)
					m.input.Model.SetCursor(len(selected))
				}
				return m, nil
			}
		}

		// Handle other keys for textarea
		oldVal := m.input.Value()
		newInput, inputCmd := m.input.Update(msg)
		m.input = newInput
		cmd = inputCmd

		// If input changed, trigger suggestion fetch with debounce
		if m.input.Value() != oldVal {
			newVal := m.input.Value()
			return m, tea.Batch(cmd, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
				return debounceMsg{value: newVal}
			}))
		}
	case tea.WindowSizeMsg:
		m.input.Model.SetWidth(msg.Width - 4)
		return m, nil

	case debounceMsg:
		// Only fetch suggestions if the value hasn't changed since the debounce message was sent
		if msg.value == m.input.Value() {
			m.cancel()
			m.ctx, m.cancel = context.WithCancel(context.Background())
			return m, m.getSuggestions(msg.value)
		}
		return m, nil

	case SuggestionsMsg:
		m.suggester.Update(msg, -1) // Reset index when new suggestions arrive
		return m, nil

	case error:
		m.err = msg
		return m, nil
	}

	return m, cmd
}

// View renders the TUI prompt layout.
func (m Model) View() string {
	if m.submitted || m.aborted {
		return ""
	}

	return modelStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			m.input.View(),
			"\n",
			m.suggester.View(),
		),
	)
}

func (m *Model) submit() {
	m.finalPrompt = m.input.Value()
	m.submitted = true
}

func (m *Model) getSuggestions(prefix string) tea.Cmd {
	return func() tea.Msg {
		// Asynchronous call to the suggestion service
		suggestions, err := m.suggestionSvc.GetSuggestions(m.ctx, prefix)
		if err != nil {
			if context.Cause(m.ctx) == context.Canceled {
				return nil // Silently ignore canceled requests
			}
			// For other errors, we could return them, but let's keep it quiet in TUI
			return nil
		}
		return SuggestionsMsg(suggestions)
	}
}

// FinalPrompt returns the captured prompt.
func (m *Model) FinalPrompt() string {
	return m.finalPrompt
}

// Aborted returns true if the user cancelled the prompt.
func (m *Model) Aborted() bool {
	return m.aborted
}
