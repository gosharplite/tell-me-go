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

// domainEventMsg wraps a domain event to ensure exactly one reader exists on
// the event channel. Native Bubble Tea messages must not trigger additional
// channel reads.
type domainEventMsg events.Event

type state int

const (
	stateIdle state = iota
	stateThinking
	stateRendering
)

type model struct {
	eventCh <-chan events.Event

	currentState state
	width        int // terminal width, updated via WindowSizeMsg
	turn         int
	modelName    string // display name, e.g. "deepseek-v4-pro"
	sessionName  string // e.g. "architect-johndoe"
	tokens       int
	maxTokens    int
	timestamp    time.Time
	err          error
	responseText    string                // accumulated AI response text
	rawResponseText string                // raw text before markdown rendering, for re-rendering on resize
	mdRender        func(string, int) string // optional markdown renderer (text, width)
	postCallStatus  *events.TurnStatus    // set when IsPostCall, has full status including Metrics and StartTime
	finalCostLine   string                // rendered "Ready (...)" line from IsFinal
}

// NewModel creates a new progress model that consumes events from the given
// channel and optionally renders response text through mdRender.
func NewModel(_ context.Context, ch <-chan events.Event, mdRender func(string, int) string) tea.Model {
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
		return domainEventMsg(e)
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
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		if m.rawResponseText != "" && m.mdRender != nil {
			m.responseText = strings.TrimRight(m.mdRender(m.rawResponseText, m.width), "\n")
		}
		return m, nil

	case error:
		m.err = msg
		return m, nil

	case domainEventMsg:
		switch e := events.Event(msg).(type) {
		case events.TurnStarted:
			m.turn = e.SessionTurns + 1
			m.currentState = stateThinking
			return m, m.waitForEvent()

		case events.InferenceStartedEvent:
			m.modelName = e.Model
			return m, m.waitForEvent()

		case events.TurnStatusEvent:
			m.turn = e.Status.SessionTurns + 1
			m.tokens = e.Status.Tokens
			m.maxTokens = e.Status.MaxHistoryTokens
			m.timestamp = e.Status.Timestamp
			m.sessionName = e.Status.Mode
			m.modelName = e.Status.Model
			m.currentState = stateRendering

			if e.Status.IsPostCall && e.Status.Metrics != nil {
				s := e.Status
				m.postCallStatus = &s
			}
			if e.Status.IsFinal {
				turnCost := 0.0
				if e.Status.Metrics != nil {
					turnCost = e.Status.Metrics.Cost
				}
				m.finalCostLine = formatFinalLine(e.Status, turnCost)
			}
			return m, m.waitForEvent()

		case events.ResponseEvent:
			m.rawResponseText = extractResponseText(e.Content)
			if m.mdRender != nil {
				m.responseText = strings.TrimRight(m.mdRender(m.rawResponseText, m.width), "\n")
			} else {
				m.responseText = m.rawResponseText
			}
			return m, m.waitForEvent()
		}
		return m, m.waitForEvent()

	default:
		return m, nil
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

// formatMetricsLine renders a single-line post-call metrics summary.
func formatMetricsLine(m *llm.Metrics, startTime time.Time, timestamp time.Time, turns int) string {
	if m == nil {
		return ""
	}
	miss := m.PromptTokens - m.CachedTokens
	var parts []string

	// Timestamp
	parts = append(parts, fmt.Sprintf("[%s]", timestamp.Format("15:04:05")))

	// Model display
	display := m.Provider
	if display == "" {
		display = m.Model
	}
	if display != "" {
		parts = append(parts, fmt.Sprintf("[%s]", display))
	}

	parts = append(parts, fmt.Sprintf("M: %d H: %d C: %d", miss, m.CachedTokens, m.ResponseTokens))

	if m.ThinkingTokens > 0 {
		parts = append(parts, fmt.Sprintf("Th: %d", m.ThinkingTokens))
	}

	if m.Cost > 0 {
		parts = append(parts, fmt.Sprintf("($%.4f)", m.Cost))
	}

	totalLatency := m.Duration + m.ToolDuration
	timing := fmt.Sprintf("[%.2fs (ΣT: %.2fs)]", totalLatency, m.CumulativeToolDuration)
	if !startTime.IsZero() {
		sessionDur := time.Since(startTime).Seconds()
		if turns > 0 {
			timing = fmt.Sprintf("%s / %.2fs (%.2f)", timing, sessionDur, sessionDur/float64(turns))
		} else {
			timing = fmt.Sprintf("%s / %.2fs", timing, sessionDur)
		}
	}
	parts = append(parts, timing)

	return strings.Join(parts, " ")
}

// formatFinalLine renders the "Ready" summary line when IsFinal is true.
func formatFinalLine(status events.TurnStatus, turnCost float64) string {
	hitRate := 0.0
	if total := status.TotalM + status.TotalH; total > 0 {
		hitRate = float64(status.TotalH) / float64(total) * 100
	}
	return fmt.Sprintf("╰─⠿ Ready ($%.4f $%.4f $%.4f $%.4f M: %d H: %d %.1f%% O: %d)",
		turnCost, status.TaskCost, status.SessionCost, status.DailyCost,
		status.TotalM, status.TotalH, hitRate, status.TotalO)
}

// View renders the progress model as a two-line display with optional response text.
func (m *model) View() string {
	var sb strings.Builder

	ts := m.timestamp.Format("15:04:05")
	sb.WriteString(fmt.Sprintf("╭─ Turn %d - %s\n", m.turn, m.sessionName))
	sb.WriteString(fmt.Sprintf("[%s] Payload: ~%d/%d tokens - %s - %s",
		ts, m.tokens, m.maxTokens, m.sessionName, m.modelName))

	if m.responseText != "" {
		sb.WriteString("\n")
		sb.WriteString(m.responseText)
	}

	if m.postCallStatus != nil {
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("[%s] Payload: %d/%d tokens - %s - %s",
			m.timestamp.Format("15:04:05"),
			m.postCallStatus.Metrics.PromptTokens,
			m.maxTokens, m.sessionName, m.modelName))
		sb.WriteString("\n")
		sb.WriteString(formatMetricsLine(m.postCallStatus.Metrics,
			m.postCallStatus.StartTime, m.timestamp, m.postCallStatus.CurrentTurns+1))
	}

	if m.finalCostLine != "" {
		sb.WriteString("\n")
		sb.WriteString(m.finalCostLine)
	}

	sb.WriteString("\n")
	return sb.String()
}
