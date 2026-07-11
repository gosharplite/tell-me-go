// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
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
}

// NewModel creates a new progress model that consumes events from the given
// channel.
func NewModel(_ context.Context, ch <-chan events.Event) tea.Model {
	return &model{
		eventCh:      ch,
		currentState: stateIdle,
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

	case error:
		m.err = msg
		return m, m.waitForEvent()

	default:
		return m, m.waitForEvent()
	}
}

// View renders the progress model as a two-line display.
func (m *model) View() string {
	ts := m.timestamp.Format("15:04:05")
	header := fmt.Sprintf("╭─ Turn %d - %s", m.turn, m.sessionName)
	info := fmt.Sprintf("[%s] Payload: ~%d/%d tokens - %s - %s",
		ts, m.tokens, m.maxTokens, m.sessionName, m.modelName)
	return header + "\n" + info + "\n" + "\n"
}
