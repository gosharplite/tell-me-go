// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

const (
	// DefaultDebounceDuration is the default delay for fetching suggestions.
	DefaultDebounceDuration = 100 * time.Millisecond
)

var (
	modelStyle = lipgloss.NewStyle().Padding(1, 1)
)

// suggestionsMsg contains a list of suggested prompts returned from an asynchronous call.
type suggestionsMsg []string

// debounceMsg is sent after a short delay to trigger suggestion fetching.
type debounceMsg struct {
	value string
}

// PromptModel defines the public interface for the interactive TUI prompt.
type PromptModel interface {
	tea.Model
	FinalPrompt() string
	Aborted() bool
	Destroy()
}

// promptModel is the main orchestrator for the TUI prompt.
type promptModel struct {
	input     textArea
	suggester suggester

	// DI
	suggestionSvc ports.SuggestionService

	// Config
	debounceDuration time.Duration

	// State
	finalPrompt string
	ctx         context.Context
	cancel      context.CancelFunc
	err         error
	submitted   bool
	aborted     bool
}

// NewModel creates a new PromptModel with the given suggestion service and debounce duration.
func NewModel(svc ports.SuggestionService, debounceDuration time.Duration) PromptModel {
	ctx, cancel := context.WithCancel(context.Background())

	if debounceDuration == 0 {
		debounceDuration = DefaultDebounceDuration
	}

	return &promptModel{
		input:            newTextArea(),
		suggester:        suggester{Index: -1}, // -1 means no suggestion is currently selected/highlighted
		suggestionSvc:    svc,
		debounceDuration: debounceDuration,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Destroy cleans up the model's resources, specifically cancelling any background context.
func (m *promptModel) Destroy() {
	if m.cancel != nil {
		m.cancel()
	}
}

// Init initializes the TUI prompt and triggers initial suggestions.
func (m promptModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.getSuggestions(m.ctx, ""), // Load initial top prompts
	)
}

// Update handles UI interactions and asynchronous messages.
func (m *promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.input.Model.SetWidth(msg.Width - 4)
		return m, nil

	case debounceMsg:
		return m.handleDebounceMsg(msg)

	case suggestionsMsg:
		m.suggester.Update(msg, -1) // Reset index when new suggestions arrive
		return m, nil

	case error:
		log.Printf("prompt model error: %v", msg)
		m.err = msg
		return m, nil
	}

	return m, nil
}

func (m *promptModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.aborted = true
		return m, tea.Quit

	case tea.KeyTab, tea.KeyShiftTab:
		if len(m.suggester.Suggestions) > 0 {
			return m.handleTabKey(msg)
		}

	case tea.KeyEnter:
		if msg.Alt {
			return m.handleSubmissionKeys(msg)
		}

	case tea.KeyCtrlS:
		return m.handleSubmissionKeys(msg)
	}

	// Handle other keys for textarea
	oldVal := m.input.Value()
	newInput, inputCmd := m.input.Update(msg)
	m.input = newInput

	// If input changed, trigger suggestion fetch with debounce
	if m.input.Value() != oldVal {
		newVal := m.input.Value()
		return m, tea.Batch(inputCmd, tea.Tick(m.debounceDuration, func(t time.Time) tea.Msg {
			return debounceMsg{value: newVal}
		}))
	}

	return m, inputCmd
}

func (m *promptModel) handleTabKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyTab {
		m.suggester.Next()
	} else {
		m.suggester.Prev()
	}

	selected := m.suggester.GetSelected()
	if selected != "" {
		currentVal := m.input.Value()
		lastSpaceIdx := strings.LastIndex(currentVal, " ")

		// Heuristic: If input has multiple words AND suggestion is a single token (like a file path),
		// replace only the last token. Otherwise, replace the entire line.
		if lastSpaceIdx != -1 && !strings.Contains(selected, " ") {
			preservedContext := currentVal[:lastSpaceIdx+1]
			m.input.SetValue(preservedContext + selected)
		} else {
			m.input.SetValue(selected)
		}

		m.input.Model.SetCursor(len(m.input.Value()))
	}
	return m, nil
}

func (m *promptModel) handleSubmissionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.submit() {
		return m, tea.Quit
	}
	return m, nil
}

func (m *promptModel) handleDebounceMsg(msg debounceMsg) (tea.Model, tea.Cmd) {
	// Only fetch suggestions if the value hasn't changed since the debounce message was sent
	if msg.value == m.input.Value() {
		m.cancel()
		m.ctx, m.cancel = context.WithCancel(context.Background())
		return m, m.getSuggestions(m.ctx, msg.value)
	}
	return m, nil
}

// View renders the TUI prompt layout.
func (m promptModel) View() string {
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

func (m *promptModel) submit() bool {
	trimmed := strings.TrimSpace(m.input.Value())
	if trimmed == "" {
		return false
	}
	m.finalPrompt = trimmed
	m.submitted = true
	return true
}

func (m *promptModel) getSuggestions(ctx context.Context, prefix string) tea.Cmd {
	return func() tea.Msg {
		// Asynchronous call to the suggestion service
		suggestions, err := m.suggestionSvc.GetSuggestions(ctx, prefix)
		if err != nil {
			if ctx.Err() != nil {
				return nil // Silently ignore canceled requests
			}
			log.Printf("failed to get suggestions: %v", err)
			return nil
		}
		// Filter out suggestions with more than 3 lines
		var filtered []string
		for _, s := range suggestions {
			if strings.Count(strings.TrimSpace(s), "\n") < 3 {
				filtered = append(filtered, s)
			}
		}

		return suggestionsMsg(filtered)
	}
}

// FinalPrompt returns the captured prompt.
func (m *promptModel) FinalPrompt() string {
	return m.finalPrompt
}

// Aborted returns true if the user cancelled the prompt.
func (m *promptModel) Aborted() bool {
	return m.aborted
}
