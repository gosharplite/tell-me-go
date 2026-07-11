// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type state int

const (
	stateIdle state = iota
	stateThinking
	stateRendering
)

type model struct {
	eventCh <-chan events.Event

	currentState state
	turn         int
	modelName    string // display name, e.g. "deepseek-v4-pro"
	sessionName  string // e.g. "architect-johndoe"
	tokens       int
	maxTokens    int
	timestamp    time.Time
	err          error
	responseText string // accumulated AI response text
	mdRender     func(string) string // optional markdown renderer
}

// NewModel creates a new progress model that consumes events from the given
// channel and optionally renders response text through mdRender.
func NewModel(_ context.Context, ch <-chan events.Event, mdRender func(string) string) tea.Model {
	return &model{
		eventCh:      ch,
		currentState: stateIdle,
		mdRender:     mdRender,
	}
}

// waitForEvent reads the next event from the channel. If the channel is
// closed, it signals the Bubbletea runtime to quit.
func (m *model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-m.eventCh
		if !ok {
			return tea.Quit()
		}
		return e
	}
}

// Init returns the initial command to start listening for events.
func (m *model) Init() tea.Cmd {
	return m.waitForEvent()
}

// Update handles incoming messages and updates the model state accordingly.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m, m.waitForEvent()

	case events.TurnStarted:
		m.turn = msg.Turn + 1
		m.currentState = stateThinking
		return m, m.waitForEvent()

	case events.InferenceStartedEvent:
		m.modelName = msg.Model
		return m, m.waitForEvent()

	case events.TurnStatusEvent:
		m.tokens = msg.Status.Tokens
		m.maxTokens = msg.Status.MaxHistoryTokens
		m.timestamp = msg.Status.Timestamp
		m.sessionName = msg.Status.Mode
		m.modelName = msg.Status.Model
		m.currentState = stateRendering
		return m, m.waitForEvent()

	case events.ResponseEvent:
		text := extractResponseText(msg.Content)
		if m.mdRender != nil {
			text = strings.TrimRight(m.mdRender(text), "\n")
		}
		m.responseText = text
		return m, m.waitForEvent()

	case error:
		m.err = msg
		return m, m.waitForEvent()

	default:
		return m, m.waitForEvent()
	}
}

// extractResponseText concatenates non-thought text parts from an LLM response.
func extractResponseText(content *llm.Content) string {
	if content == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range content.Parts {
		if part.Text != "" && !part.IsThought {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

// View renders the progress model as a two-line display with optional response text.
func (m *model) View() string {
	ts := m.timestamp.Format("15:04:05")
	header := fmt.Sprintf("╭─ Turn %d - %s", m.turn, m.sessionName)
	info := fmt.Sprintf("[%s] Payload: ~%d/%d tokens - %s - %s",
		ts, m.tokens, m.maxTokens, m.sessionName, m.modelName)
	out := header + "\n" + info
	if m.responseText != "" {
		out += "\n" + m.responseText
	}
	return out + "\n"
}
