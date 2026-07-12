// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"sort"
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
type spinnerTickMsg struct {
	generation int
}

// channelClosedMsg signals that the event channel has been closed (session complete).
type channelClosedMsg struct{}

type state int

const (
	stateIdle state = iota
	stateThinking
	stateRendering
)

var brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerTickInterval = 200 * time.Millisecond

// spinnerState encapsulates the animated braille spinner state and lifecycle.
// It is embedded in the model to keep spinner concerns separate from domain state.
type spinnerState struct {
	status     string
	frame      int
	tickActive bool
	startTime  time.Time
	generation int
}

// start initializes the spinner for a new phase. Returns a tick command
// (or nil if a tick is already active from a previous phase).
func (s *spinnerState) start(status string) tea.Cmd {
	s.status = status
	s.frame = 0
	s.startTime = time.Now()
	s.generation++
	if !s.tickActive {
		return s.tick()
	}
	return nil
}

// tick schedules the next spinner frame tick, or returns nil if the
// spinner has been cleared.
func (s *spinnerState) tick() tea.Cmd {
	if s.status == "" {
		s.tickActive = false
		return nil
	}
	s.tickActive = true
	gen := s.generation
	return tea.Tick(spinnerTickInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg{generation: gen}
	})
}

// handleTick processes a tick message. Returns the next tick command,
// or nil if the tick is stale (wrong generation) or spinner is inactive.
func (s *spinnerState) handleTick(msg spinnerTickMsg) tea.Cmd {
	if msg.generation != s.generation {
		return nil // stale tick from a previous turn
	}
	s.frame = (s.frame + 1) % len(brailleFrames)
	s.tickActive = false
	return s.tick()
}

// clear stops the spinner and resets all state.
func (s *spinnerState) clear() {
	s.status = ""
	s.tickActive = false
}

// render returns the spinner line for display, including the current
// braille frame, status text, and elapsed seconds.
func (s *spinnerState) render() string {
	frame := brailleFrames[s.frame%len(brailleFrames)]
	elapsed := int(time.Since(s.startTime).Seconds())
	return fmt.Sprintf("\n%s %s (%ds)", frame, s.status, elapsed)
}

// active reports whether the spinner is currently running.
func (s *spinnerState) active() bool {
	return s.status != ""
}

type model struct {
	eventCh <-chan events.Event

	currentState        state
	width               int // terminal width, updated via WindowSizeMsg
	height              int // terminal height, updated via WindowSizeMsg
	turn                int
	modelName           string // display name, e.g. "deepseek-v4-pro"
	sessionName         string // e.g. "architect-johndoe"
	tokens              int
	maxTokens           int
	timestamp           time.Time
	err                 error
	responseText        string                   // accumulated AI response text
	rawResponseText     string                   // raw text before markdown rendering, for re-rendering on resize
	mdRender            func(string, int) string // optional markdown renderer (text, width)
	postCallStatus      *events.TurnStatus       // set when IsPostCall, has full status including Metrics and StartTime
	postCallMetricsLine string                   // pre-rendered metrics line, frozen when IsPostCall fires
	finalCostLine       string                   // rendered "Ready (...)" line from IsFinal
	sessionDone         bool                     // true when event channel closes; TUI waits for Ctrl+C
	spinner             spinnerState

	toolLogs    []string        // accumulated tool call/result/output lines, cleared each turn
	seenCallIDs map[string]bool // dedup tool logs from ResponseEvent vs ToolCallEvent
}

// NewModel creates a new progress model that consumes events from the given
// channel and optionally renders response text through mdRender.
func NewModel(_ context.Context, ch <-chan events.Event, mdRender func(string, int) string) tea.Model {
	return &model{
		eventCh:      ch,
		currentState: stateIdle,
		height:       24, // sensible default before first WindowSizeMsg
		mdRender:     mdRender,
		seenCallIDs:  make(map[string]bool),
	}
}

// waitForEvent reads the next event from the channel. If the channel is
// closed, it signals that the session is complete (screen stays open for review).
func (m *model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-m.eventCh
		if !ok {
			return channelClosedMsg{}
		}
		return domainEventMsg(e)
	}
}

// appendToolLog appends a timestamped log line for tool events.
func (m *model) appendToolLog(tag, message string) {
	ts := time.Now().Format("15:04:05")
	m.toolLogs = append(m.toolLogs, fmt.Sprintf("[%s] [%s] %s", ts, tag, message))
}

// appendLevelEventLog appends a timestamped log line with a level-mapped
// prefix. Used by ToolOutputStreamEvent, SystemMessageEvent, and StatusUpdate.
func (m *model) appendLevelEventLog(level, message string) {
	prefix := "System"
	switch level {
	case "error":
		prefix = "Error"
	case "warn":
		prefix = "Warning"
	case "output":
		prefix = "Tool Output"
	case "info":
		prefix = "Info"
	}
	m.appendToolLog(prefix, message)
}

// safeTruncate truncates s to at most maxLen bytes, ensuring the cut
// never lands in the middle of a multi-byte UTF-8 character.
// If s is already shorter than maxLen, it is returned unchanged.
func safeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Walk runes to find the byte index of the (maxLen)th rune.
	var count, byteIdx int
	for i := range s {
		if count == maxLen {
			byteIdx = i
			break
		}
		count++
	}
	if byteIdx > 0 {
		return s[:byteIdx]
	}
	// maxLen is 0 or s starts with a multi-byte rune exceeding maxLen.
	return ""
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
		return m, tea.Batch(m.spinner.handleTick(msg), m.waitForEvent())
	case channelClosedMsg:
		m.sessionDone = true
		m.spinner.clear()
		return m, nil
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
	m.height = msg.Height
	if m.rawResponseText != "" && m.mdRender != nil {
		m.responseText = strings.TrimRight(m.mdRender(m.rawResponseText, m.width), "\n")
	}
	return m, nil
}

// spinnerInfoProvider is a local interface capturing events that can drive
// a spinner. It unifies SummarizationStartedEvent, ToolExecutionStartedEvent,
// and RetryWaitingEvent, which all have the identical SpinnerInfo() pattern.
type spinnerInfoProvider interface {
	SpinnerInfo() (events.SpinnerInfo, bool)
}

// handleDomainEvent dispatches a domain event to the appropriate handler.
func (m *model) handleDomainEvent(msg domainEventMsg) (tea.Model, tea.Cmd) {
	switch e := events.Event(msg).(type) {
	case events.ToolCallEvent:
		return m, m.handleToolCallEvent(e)
	case events.ToolResultEvent:
		return m, m.handleToolResultEvent(e)
	case events.ToolOutputStreamEvent:
		return m, m.handleToolOutputStreamEvent(e)
	case events.SystemMessageEvent:
		return m, m.handleLeveledMessage(e.Level, e.Message)
	case events.StatusUpdate:
		return m, m.handleLeveledMessage(e.Level, e.Message)
	case events.TurnStarted:
		return m, m.handleTurnStarted(e)
	case events.InferenceStartedEvent:
		return m, m.handleInferenceStarted(e)
	case events.TurnStatusEvent:
		return m, m.handleTurnStatus(e)
	case events.ResponseEvent:
		return m, m.handleResponseEvent(e)
	case events.SummarizationStartedEvent,
		events.ToolExecutionStartedEvent,
		events.RetryWaitingEvent:
		return m, m.handleSpinnerEvent(e.(spinnerInfoProvider))
	}
	return m, m.waitForEvent()
}

// handleLeveledMessage logs a leveled system message and returns a
// waitForEvent command. It is used by both SystemMessageEvent and
// StatusUpdate handlers.
func (m *model) handleLeveledMessage(level, message string) tea.Cmd {
	m.appendLevelEventLog(level, message)
	return m.waitForEvent()
}

// handleTurnStarted processes a TurnStarted event.
func (m *model) handleTurnStarted(e events.TurnStarted) tea.Cmd {
	m.turn = e.SessionTurns + 1
	m.currentState = stateThinking
	m.spinner.clear()
	m.seenCallIDs = make(map[string]bool)
	m.responseText = ""
	m.rawResponseText = ""
	return m.waitForEvent()
}

// handleInferenceStarted processes an InferenceStartedEvent.
func (m *model) handleInferenceStarted(e events.InferenceStartedEvent) tea.Cmd {
	m.modelName = e.Model
	info, _ := e.SpinnerInfo()
	return tea.Batch(m.waitForEvent(), m.spinner.start(info.Status))
}

// handleTurnStatus processes a TurnStatusEvent.
func (m *model) handleTurnStatus(e events.TurnStatusEvent) tea.Cmd {
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
	if m.spinner.active() {
		m.spinner.clear()
	}
	return m.waitForEvent()
}

// handleResponseEvent processes a ResponseEvent.
func (m *model) handleResponseEvent(e events.ResponseEvent) tea.Cmd {
	m.spinner.clear()
	m.rawResponseText = extractResponseText(e.Content)
	if m.mdRender != nil {
		m.responseText = strings.TrimRight(m.mdRender(m.rawResponseText, m.width), "\n")
	} else {
		m.responseText = m.rawResponseText
	}

	// Extract tool call info from ResponseEvent so [Tool Reason]
	// and [Tool Action] appear immediately — before ToolCallEvent
	// arrives from the Dispatcher.
	if e.Content != nil {
		m.extractToolCallsFromResponse(e.Content)
	}

	return m.waitForEvent()
}

// handleToolCallEvent processes a ToolCallEvent, deduplicating calls already
// seen via ResponseEvent and logging tool reasons and actions.
func (m *model) handleToolCallEvent(e events.ToolCallEvent) tea.Cmd {
	if len(e.Calls) == 0 {
		return m.waitForEvent()
	}
	newCalls := make([]*llm.FunctionCall, 0, len(e.Calls))
	for _, fc := range e.Calls {
		id := fc.ID
		if id == "" {
			id = fc.Name
		}
		if !m.seenCallIDs[id] {
			newCalls = append(newCalls, fc)
		}
	}
	if len(newCalls) > 0 {
		m.appendToolLog("Tool Engine", fmt.Sprintf("Step %d/%d", e.Turn+1, e.MaxTurns))
	}
	for _, fc := range newCalls {
		m.logToolCall(fc)
	}
	return m.waitForEvent()
}

// logToolCall logs a single function call's reason and action to the
// tool log, deduplicating by call ID. Calls already logged (tracked in
// seenCallIDs) are silently skipped.
func (m *model) logToolCall(fc *llm.FunctionCall) {
	id := fc.ID
	if id == "" {
		id = fc.Name
	}
	if m.seenCallIDs[id] {
		return
	}
	m.seenCallIDs[id] = true

	if reason, ok := fc.Args["reason"].(string); ok && reason != "" {
		m.appendToolLog("Tool Reason", reason)
	}

	var keys []string
	for k := range fc.Args {
		if k != "reason" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		valStr := fmt.Sprintf("%v", fc.Args[k])
		if len(valStr) > 189 {
			valStr = safeTruncate(valStr, 186) + "..."
		}
		parts = append(parts, fmt.Sprintf("%s: %s", k, valStr))
	}
	m.appendToolLog("Tool Action", fmt.Sprintf("%s(%s)", fc.Name, strings.Join(parts, ", ")))
}

// handleToolResultEvent logs a truncated result snippet for a completed tool call.
func (m *model) handleToolResultEvent(e events.ToolResultEvent) tea.Cmd {
	if e.Name == "" {
		return m.waitForEvent()
	}
	if e.Result.Text != "" {
		snippet := e.Result.Text
		if len(snippet) > 200 {
			snippet = safeTruncate(snippet, 197) + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		m.appendToolLog("Tool Result", fmt.Sprintf("%s: %s", e.Name, snippet))
	}
	return m.waitForEvent()
}

// handleToolOutputStreamEvent logs tool output stream messages, suppressing
// plain "output" level messages that are handled elsewhere.
func (m *model) handleToolOutputStreamEvent(e events.ToolOutputStreamEvent) tea.Cmd {
	if e.Level == "output" {
		return m.waitForEvent()
	}
	m.appendLevelEventLog(e.Level, e.Message)
	return m.waitForEvent()
}

// handleSpinnerEvent starts the spinner with the status text from a
// spinner-bearing event (SummarizationStartedEvent, ToolExecutionStartedEvent,
// RetryWaitingEvent).
func (m *model) handleSpinnerEvent(e spinnerInfoProvider) tea.Cmd {
	info, _ := e.SpinnerInfo()
	return tea.Batch(m.waitForEvent(), m.spinner.start(info.Status))
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

// extractToolCallsFromResponse populates toolLogs from FunctionCall parts
// in the ResponseEvent content. This bridges the visibility gap between
// ResponseEvent and ToolCallEvent. seenCallIDs prevents duplicates when
// the real ToolCallEvent arrives later from the Dispatcher.
func (m *model) extractToolCallsFromResponse(content *llm.Content) {
	var toolCalls []*llm.FunctionCall
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, part.FunctionCall)
		}
	}
	if len(toolCalls) == 0 {
		return
	}
	for _, fc := range toolCalls {
		m.logToolCall(fc)
	}
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

// View renders the progress model as a three-zone fixed layout:
// header (2 lines), scrollable body, footer (3 lines).
// Falls back to renderMinimal when the terminal is too small (height < 5).
func (m *model) View() string {
	if m.height < 8 {
		return m.renderMinimal()
	}

	// Body gets everything between header+gap (3 lines) and gap+footer (5 lines).
	availableBody := m.height - 8

	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")
	sb.WriteString(m.renderBody(availableBody))
	sb.WriteString("\n")
	sb.WriteString(m.renderFooter())
	return sb.String()
}

// renderMinimal returns a single-line fallback for tiny terminals.
func (m *model) renderMinimal() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("╭─ Turn %d - %s", m.turn, m.sessionName))
	if m.sessionDone {
		sb.WriteString(" Press Ctrl+C to exit")
	} else if m.spinner.active() && m.currentState != stateIdle {
		frame := brailleFrames[m.spinner.frame%len(brailleFrames)]
		elapsed := int(time.Since(m.spinner.startTime).Seconds())
		sb.WriteString(fmt.Sprintf(" %s %s (%ds)", frame, m.spinner.status, elapsed))
	}
	return sb.String()
}

// renderHeader returns the turn header and payload line (2 lines, always present).
func (m *model) renderHeader() string {
	ts := m.timestamp.Format("15:04:05")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("╭─ Turn %d - %s\n", m.turn, m.sessionName))
	sb.WriteString(fmt.Sprintf("[%s] Payload: ~%d/%d tokens - %s - %s",
		ts, m.tokens, m.maxTokens, m.sessionName, m.modelName))
	return sb.String()
}

// renderBody returns exactly availableLines of content (tool logs + response),
// top-aligned with a blank separator line first, height-constrained.
// Content fills from the top downward; blank lines pad the bottom.
// When overflowing, the oldest entry at the top is pushed off.
func (m *model) renderBody(availableLines int) string {
	var contentLines []string

	for _, log := range m.toolLogs {
		for _, subLine := range strings.Split(log, "\n") {
			clipped := subLine
			if m.width > 0 && len([]rune(clipped)) > m.width {
				clipped = string([]rune(clipped)[:m.width-3]) + "..."
			}
			contentLines = append(contentLines, clipped)
		}
	}
	if m.responseText != "" {
		for _, line := range strings.Split(m.responseText, "\n") {
			contentLines = append(contentLines, line)
		}
	}

	bodyLines := make([]string, 0, availableLines)

	availableForContent := availableLines
	if len(contentLines) > availableForContent {
		// Keep only the tail (newest content at bottom, oldest pushed off top).
		bodyLines = append(bodyLines, contentLines[len(contentLines)-availableForContent:]...)
	} else {
		// Bottom-pad with blank lines so footer stays fixed.
		bodyLines = append(bodyLines, contentLines...)
		padding := availableForContent - len(contentLines)
		for i := 0; i < padding; i++ {
			bodyLines = append(bodyLines, "")
		}
	}

	if len(bodyLines) == 0 {
		return ""
	}
	return "\n" + strings.Join(bodyLines, "\n")
}

// renderFooter returns exactly 4 lines: post-call payload, metrics, final
// cost summary, and spinner. Each line is either its real content or blank.
func (m *model) renderFooter() string {
	var sb strings.Builder

	// Line 1: post-call payload (exact token count, separate from header).
	sb.WriteString("\n")
	if m.postCallStatus != nil {
		sb.WriteString(fmt.Sprintf("[%s] Payload: %d/%d tokens - %s - %s",
			m.timestamp.Format("15:04:05"),
			m.postCallStatus.Metrics.PromptTokens,
			m.maxTokens, m.sessionName, m.modelName))
	}

	// Line 2: post-call metrics.
	sb.WriteString("\n")
	if m.postCallMetricsLine != "" {
		sb.WriteString(m.postCallMetricsLine)
	}

	// Line 3: final cost summary.
	sb.WriteString("\n")
	if m.finalCostLine != "" {
		sb.WriteString(m.finalCostLine)
	}

	// Line 4: spinner or exit prompt.
	sb.WriteString("\n")
	if m.sessionDone {
		sb.WriteString("Press Ctrl+C to exit")
	} else if m.spinner.active() && m.currentState != stateIdle {
		frame := brailleFrames[m.spinner.frame%len(brailleFrames)]
		elapsed := int(time.Since(m.spinner.startTime).Seconds())
		sb.WriteString(fmt.Sprintf("%s %s (%ds)", frame, m.spinner.status, elapsed))
	}

	return sb.String()
}
