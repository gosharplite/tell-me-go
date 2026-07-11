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

// spinnerTickMsg is an internal message type for spinner frame ticks.
type spinnerTickMsg time.Time

type state int

const (
	stateIdle state = iota
	stateThinking
	stateRendering
)

var brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerTickInterval = 200 * time.Millisecond

type model struct {
	eventCh <-chan events.Event

	currentState       state
	width              int // terminal width, updated via WindowSizeMsg
	turn               int
	modelName          string // display name, e.g. "deepseek-v4-pro"
	sessionName        string // e.g. "architect-johndoe"
	tokens             int
	maxTokens          int
	timestamp          time.Time
	err                error
	responseText       string                   // accumulated AI response text
	rawResponseText    string                   // raw text before markdown rendering, for re-rendering on resize
	mdRender           func(string, int) string // optional markdown renderer (text, width)
	postCallStatus      *events.TurnStatus       // set when IsPostCall, has full status including Metrics and StartTime
	postCallMetricsLine string                   // pre-rendered metrics line, frozen when IsPostCall fires
	finalCostLine       string                   // rendered "Ready (...)" line from IsFinal
	spinnerStatus      string                   // current spinner text, e.g. " Executing [bash]..."
	spinnerShowMetrics bool                     // if true, metrics (CPU/MEM) should be shown alongside spinner
	spinnerFrame       int                      // index into brailleFrames, incremented each tick
	spinnerTickActive  bool                     // true when a tick command is pending
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

// spinnerTick returns a command that fires a tick and advances the spinner frame,
// or nil if no spinner is active.
func (m *model) spinnerTick() tea.Cmd {
	if m.spinnerStatus == "" || m.currentState == stateIdle {
		m.spinnerTickActive = false
		return nil
	}
	m.spinnerTickActive = true
	return tea.Tick(spinnerTickInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// Init returns the initial command to start listening for events.
func (m *model) Init() tea.Cmd {
	return m.waitForEvent()
}

// Update handles incoming messages and updates the model state accordingly.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case spinnerTickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(brailleFrames)
		m.spinnerTickActive = false
		return m, m.spinnerTick()
	case error:
		m.err = msg
		return m, nil
	case domainEventMsg:
		return m.handleDomainEvent(msg)
	default:
		return m, nil
	}
}

// handleKeyMsg processes keyboard messages.
func (m *model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	return m, nil
}

// handleWindowSizeMsg processes terminal resize events.
func (m *model) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	if m.rawResponseText != "" && m.mdRender != nil {
		m.responseText = strings.TrimRight(m.mdRender(m.rawResponseText, m.width), "\n")
	}
	return m, nil
}

// handleDomainEvent dispatches a domain event to the appropriate handler.
func (m *model) handleDomainEvent(msg domainEventMsg) (tea.Model, tea.Cmd) {
	switch e := events.Event(msg).(type) {
	case events.TurnStarted:
		return m, m.handleTurnStarted(e)
	case events.InferenceStartedEvent:
		return m, m.handleInferenceStarted(e)
	case events.TurnStatusEvent:
		return m, m.handleTurnStatus(e)
	case events.ResponseEvent:
		return m, m.handleResponseEvent(e)
	case events.SummarizationStartedEvent:
		info, _ := e.SpinnerInfo()
		m.spinnerStatus = info.Status
		m.spinnerShowMetrics = info.WithMetrics
		m.spinnerFrame = 0
		if !m.spinnerTickActive {
			return m, tea.Batch(m.waitForEvent(), m.spinnerTick())
		}
		return m, m.waitForEvent()
	case events.ToolExecutionStartedEvent:
		info, _ := e.SpinnerInfo()
		m.spinnerStatus = info.Status
		m.spinnerShowMetrics = info.WithMetrics
		m.spinnerFrame = 0
		if !m.spinnerTickActive {
			return m, tea.Batch(m.waitForEvent(), m.spinnerTick())
		}
		return m, m.waitForEvent()
	case events.RetryWaitingEvent:
		info, _ := e.SpinnerInfo()
		m.spinnerStatus = info.Status
		m.spinnerShowMetrics = info.WithMetrics
		m.spinnerFrame = 0
		if !m.spinnerTickActive {
			return m, tea.Batch(m.waitForEvent(), m.spinnerTick())
		}
		return m, m.waitForEvent()
	}
	return m, m.waitForEvent()
}

// handleTurnStarted processes a TurnStarted event.
func (m *model) handleTurnStarted(e events.TurnStarted) tea.Cmd {
	m.turn = e.SessionTurns + 1
	m.currentState = stateThinking
	m.spinnerStatus = ""
	m.spinnerShowMetrics = false
	m.spinnerTickActive = false
	return m.waitForEvent()
}

// handleInferenceStarted processes an InferenceStartedEvent.
func (m *model) handleInferenceStarted(e events.InferenceStartedEvent) tea.Cmd {
	m.modelName = e.Model
	info, _ := e.SpinnerInfo()
	m.spinnerStatus = info.Status
	m.spinnerShowMetrics = info.WithMetrics
	m.spinnerFrame = 0
	if !m.spinnerTickActive {
		return tea.Batch(m.waitForEvent(), m.spinnerTick())
	}
	return m.waitForEvent()
}

// handleTurnStatus processes a TurnStatusEvent.
func (m *model) handleTurnStatus(e events.TurnStatusEvent) tea.Cmd {
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
		m.postCallMetricsLine = formatMetricsLine(
			s.Metrics, s.StartTime, s.Timestamp, s.CurrentTurns+1,
		)
	}
	if e.Status.IsFinal {
		turnCost := 0.0
		if e.Status.Metrics != nil {
			turnCost = e.Status.Metrics.Cost
		}
		m.finalCostLine = formatFinalLine(e.Status, turnCost)
	}
	m.spinnerStatus = ""
	m.spinnerTickActive = false
	return m.waitForEvent()
}

// handleResponseEvent processes a ResponseEvent.
func (m *model) handleResponseEvent(e events.ResponseEvent) tea.Cmd {
	m.spinnerStatus = ""
	m.spinnerTickActive = false
	m.rawResponseText = extractResponseText(e.Content)
	if m.mdRender != nil {
		m.responseText = strings.TrimRight(m.mdRender(m.rawResponseText, m.width), "\n")
	} else {
		m.responseText = m.rawResponseText
	}
	return m.waitForEvent()
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
		sb.WriteString(m.postCallMetricsLine)
	}

	if m.finalCostLine != "" {
		sb.WriteString("\n")
		sb.WriteString(m.finalCostLine)
	}

	if m.spinnerStatus != "" && m.currentState != stateIdle {
		frame := brailleFrames[m.spinnerFrame%len(brailleFrames)]
		sb.WriteString(fmt.Sprintf("\n%s %s", frame, m.spinnerStatus))
	}

	sb.WriteString("\n")
	return sb.String()
}
