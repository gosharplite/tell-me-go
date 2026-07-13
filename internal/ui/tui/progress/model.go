// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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

// mdRenderCompleteMsg signals that an asynchronous markdown rendering task has finished.
type mdRenderCompleteMsg struct {
	generation int
	rendered   string
}

type state int

const (
	stateIdle state = iota
	stateThinking
	stateRendering
)

var brailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerTickInterval = 200 * time.Millisecond

// spinnerState encapsulates the animated braille spinner state and lifecycle.
type spinnerState struct {
	status      string
	frame       int
	tickActive  bool
	startTime   time.Time
	generation  int
	showMetrics bool // true when WithMetrics was set in the triggering event
}

// start initializes the spinner for a new phase. Returns a tick command
// (or nil if a tick is already active from a previous phase).
func (s *spinnerState) start(status string, showMetrics bool) tea.Cmd {
	s.status = status
	s.frame = 0
	s.startTime = time.Now()
	s.generation++
	s.showMetrics = showMetrics
	return s.tick()
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
		s.tickActive = false
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
	s.showMetrics = false
}

// render returns the spinner line for display, including the current
// braille frame, status text, and elapsed seconds.
func (s *spinnerState) render(cpu, mem float64) string {
	frame := brailleFrames[s.frame%len(brailleFrames)]
	elapsed := int(time.Since(s.startTime).Seconds())
	if s.showMetrics {
		return fmt.Sprintf("%s %s (%ds) [CPU: %.1f%% | MEM: %.1f%%]", frame, s.status, elapsed, cpu, mem)
	}
	return fmt.Sprintf("%s %s (%ds)", frame, s.status, elapsed)
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
	responseText        string // accumulated AI response text
	rawResponseText     string // raw text before markdown rendering, for re-rendering on resize
	renderGeneration    int    // incremented on each async render request to discard stale results
	isDark              bool   // cached terminal background theme (legacy, now unused as we default to dark)
	metricsProvider     ports.SystemMetricsProvider
	lastCPUTime         int64
	lastIdleTime        int64
	lastSampleTime      time.Time
	lastCPUPercent      float64
	lastMemPercent      float64
	postCallStatus      *events.TurnStatus // set when IsPostCall, has full status including Metrics and StartTime
	postCallMetricsLine string             // pre-rendered metrics line, frozen when IsPostCall fires
	finalCostLine       string             // rendered "Ready (...)" line from IsFinal
	sessionComplete     bool               // true when event channel closes (true session end)
	spinner             spinnerState

	toolLogs    []string        // accumulated tool call/result/output lines, cleared each turn
	seenCallIDs map[string]bool // dedup tool logs from ResponseEvent vs ToolCallEvent

	headerVP viewport.Model
	bodyVP   viewport.Model
	footerVP viewport.Model
}

// resolveIsDark evaluates the GLAMOUR_STYLE environment variable to determine
// if the dark theme should be used. It explicitly avoids termenv's active IO
// probes (which cause race conditions with Bubble Tea's input loop).
func resolveIsDark() bool {
	style := os.Getenv("GLAMOUR_STYLE")
	return style == "" || style == "auto" || style == "dark"
}

// NewModel creates a new progress model that consumes events from the given
// channel.
func NewModel(_ context.Context, ch <-chan events.Event, metricsProvider ports.SystemMetricsProvider) tea.Model {
	headerVP := viewport.New(80, 2)
	bodyVP := viewport.New(80, 16)
	footerVP := viewport.New(80, 4)

	return &model{
		eventCh:         ch,
		currentState:    stateIdle,
		height:          24,
		width:           80,
		isDark:          resolveIsDark(),
		metricsProvider: metricsProvider,
		seenCallIDs:     make(map[string]bool),
		headerVP:        headerVP,
		bodyVP:          bodyVP,
		footerVP:        footerVP,
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
func safeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
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
		if m.sessionComplete {
			return m, nil // terminal state reached; suppress all ticks
		}
		if m.spinner.showMetrics {
			m.sampleMetrics(time.Now())
		}
		return m, m.spinner.handleTick(msg)
	case mdRenderCompleteMsg:
		if msg.generation != m.renderGeneration {
			return m, nil // stale: resized, new turn, or post-final
		}
		m.responseText = msg.rendered
		m.bodyVP.GotoBottom()
		return m, nil
	case channelClosedMsg:
		m.sessionComplete = true
		return m, tea.Quit
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
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	}

	return m, nil
}

// handleWindowSizeMsg processes terminal resize events.
func (m *model) handleWindowSizeMsg(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.headerVP.Width = msg.Width
	m.headerVP.Height = 2
	m.bodyVP.Width = msg.Width
	if msg.Height > 8 {
		m.bodyVP.Height = msg.Height - 8
	} else {
		m.bodyVP.Height = 0
	}
	m.footerVP.Width = msg.Width
	m.footerVP.Height = 4

	var cmd tea.Cmd
	if m.rawResponseText != "" {
		m.renderGeneration++
		m.responseText = m.rawResponseText // instant plaintext fallback
		cmd = m.renderMarkdownAsync(m.rawResponseText, m.width, m.renderGeneration)
	}
	return m, cmd
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
// waitForEvent command.
func (m *model) handleLeveledMessage(level, message string) tea.Cmd {
	m.appendLevelEventLog(level, message)
	m.bodyVP.GotoBottom()
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
	return tea.Batch(m.waitForEvent(), m.spinner.start(info.Status, info.WithMetrics))
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
		m.postCallMetricsLine = FormatMetricsLine(s)
	}
	if e.Status.IsFinal {
		turnCost := 0.0
		if e.Status.Metrics != nil {
			turnCost = e.Status.Metrics.Cost
		}
		m.finalCostLine = FormatFinalLine(e.Status, turnCost)
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

	m.renderGeneration++
	m.responseText = m.rawResponseText // instant plaintext fallback
	renderCmd := m.renderMarkdownAsync(m.rawResponseText, m.width, m.renderGeneration)

	if e.Content != nil {
		m.extractToolCallsFromResponse(e.Content)
	}
	m.bodyVP.GotoBottom()
	return tea.Batch(m.waitForEvent(), renderCmd)
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
	m.bodyVP.GotoBottom()
	return m.waitForEvent()
}

// logToolCall logs a single function call's reason and action to the
// tool log, deduplicating by call ID.
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
	m.bodyVP.GotoBottom()
	return m.waitForEvent()
}

// handleToolOutputStreamEvent logs tool output stream messages.
func (m *model) handleToolOutputStreamEvent(e events.ToolOutputStreamEvent) tea.Cmd {
	m.appendLevelEventLog(e.Level, e.Message)
	m.bodyVP.GotoBottom()
	return m.waitForEvent()
}

// handleSpinnerEvent starts the spinner with the status text from a
// spinner-bearing event.
func (m *model) handleSpinnerEvent(e spinnerInfoProvider) tea.Cmd {
	info, _ := e.SpinnerInfo()
	return tea.Batch(m.waitForEvent(), m.spinner.start(info.Status, info.WithMetrics))
}

// sampleMetrics samples CPU and memory usage from the metrics provider.
// Metrics are throttled to at most once per second; calls within the throttle
// window return the cached values. Returns zeroes when the provider is nil.
func (m *model) sampleMetrics(now time.Time) (cpu float64, mem float64) {
	if m.metricsProvider == nil {
		return 0, 0
	}
	if now.Sub(m.lastSampleTime) < time.Second && !m.lastSampleTime.IsZero() {
		return m.lastCPUPercent, m.lastMemPercent
	}
	currentTotal, currentIdle := m.metricsProvider.GetCPUStats()
	currentMem := m.metricsProvider.GetMemoryPercent()
	if !m.lastSampleTime.IsZero() && currentIdle > 0 {
		dTotal := float64(currentTotal - m.lastCPUTime)
		dIdle := float64(currentIdle - m.lastIdleTime)
		if dTotal > 0 {
			m.lastCPUPercent = (1.0 - (dIdle / dTotal)) * 100.0
		}
	}
	m.lastCPUTime = currentTotal
	m.lastIdleTime = currentIdle
	m.lastSampleTime = now
	m.lastMemPercent = currentMem
	return m.lastCPUPercent, m.lastMemPercent
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
// in the ResponseEvent content.
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

// FormatMetricsLine renders a single-line post-call metrics summary from
// a TurnStatus. Returns an empty string if Metrics is nil.
func FormatMetricsLine(ts events.TurnStatus) string {
	if ts.Metrics == nil {
		return ""
	}
	miss := ts.Metrics.PromptTokens - ts.Metrics.CachedTokens
	var parts []string

	parts = append(parts, fmt.Sprintf("[%s]", ts.Timestamp.Format("15:04:05")))

	display := ts.Metrics.Provider
	if display == "" {
		display = ts.Metrics.Model
	}
	if display != "" {
		parts = append(parts, fmt.Sprintf("[%s]", display))
	}

	parts = append(parts, fmt.Sprintf("M: %d H: %d C: %d",
		miss, ts.Metrics.CachedTokens, ts.Metrics.ResponseTokens))

	if ts.Metrics.ThinkingTokens > 0 {
		parts = append(parts, fmt.Sprintf("Th: %d", ts.Metrics.ThinkingTokens))
	}

	if ts.Metrics.Cost > 0 {
		parts = append(parts, fmt.Sprintf("($%.4f)", ts.Metrics.Cost))
	}

	totalLatency := ts.Metrics.Duration + ts.Metrics.ToolDuration
	timing := fmt.Sprintf("[%.2fs (ΣT: %.2fs)]",
		totalLatency, ts.Metrics.CumulativeToolDuration)
	if !ts.StartTime.IsZero() {
		sessionDur := time.Since(ts.StartTime).Seconds()
		turns := ts.CurrentTurns + 1
		if turns > 0 {
			timing = fmt.Sprintf("%s / %.2fs (%.2f)",
				timing, sessionDur, sessionDur/float64(turns))
		} else {
			timing = fmt.Sprintf("%s / %.2fs", timing, sessionDur)
		}
	}
	parts = append(parts, timing)

	return strings.Join(parts, " ")
}

// FormatFinalLine renders the "Ready" summary line when the session is
// complete. turnCost is the cost of the current turn (from Metrics.Cost).
func FormatFinalLine(ts events.TurnStatus, turnCost float64) string {
	hitRate := 0.0
	if total := ts.TotalM + ts.TotalH; total > 0 {
		hitRate = float64(ts.TotalH) / float64(total) * 100
	}
	return fmt.Sprintf("╰─⠿ Ready ($%.4f $%.4f $%.4f $%.4f M: %d H: %d %.1f%% O: %d)",
		turnCost, ts.TaskCost, ts.SessionCost, ts.DailyCost,
		ts.TotalM, ts.TotalH, hitRate, ts.TotalO)
}

// View renders the progress model as three viewport zones.
func (m *model) View() string {
	m.headerVP.SetContent(m.renderHeader())
	m.bodyVP.SetContent(m.renderBodyContent())
	m.footerVP.SetContent(m.renderFooterContent())

	if m.height < 8 {
		return m.renderMinimal()
	}

	return m.headerVP.View() + "\n\n" + m.bodyVP.View() + "\n\n" + m.footerVP.View()
}

// renderMinimal returns a single-line fallback for tiny terminals.
func (m *model) renderMinimal() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("╭─ Turn %d - %s", m.turn, m.sessionName))
	if m.spinner.active() && m.currentState != stateIdle {
		cpu, mem := m.lastCPUPercent, m.lastMemPercent
		sb.WriteString(fmt.Sprintf(" %s", m.spinner.render(cpu, mem)))
	}
	return sb.String()
}

// renderHeader returns the turn header and payload line.
func (m *model) renderHeader() string {
	ts := m.timestamp.Format("15:04:05")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("╭─ Turn %d - %s\n", m.turn, m.sessionName))
	sb.WriteString(fmt.Sprintf("[%s] Payload: ~%d/%d tokens - %s - %s",
		ts, m.tokens, m.maxTokens, m.sessionName, m.modelName))
	return sb.String()
}

// renderBodyContent returns tool logs and response text as plain content.
// The viewport handles overflow and scrolling.
func (m *model) renderBodyContent() string {
	var sb strings.Builder
	for _, log := range m.toolLogs {
		sb.WriteString(log)
		sb.WriteString("\n")
	}
	if m.responseText != "" {
		sb.WriteString(m.responseText)
	}
	return sb.String()
}

// renderFooterContent returns footer lines as plain content.
func (m *model) renderFooterContent() string {
	var sb strings.Builder
	if m.postCallStatus != nil {
		sb.WriteString(fmt.Sprintf("[%s] Payload: %d/%d tokens - %s - %s\n",
			m.timestamp.Format("15:04:05"),
			m.postCallStatus.Metrics.PromptTokens,
			m.maxTokens, m.sessionName, m.modelName))
	}
	if m.postCallMetricsLine != "" {
		sb.WriteString(m.postCallMetricsLine)
		sb.WriteString("\n")
	}
	if m.finalCostLine != "" {
		sb.WriteString(m.finalCostLine)
		sb.WriteString("\n")
	}
	if m.spinner.active() && m.currentState != stateIdle {
		cpu, mem := m.lastCPUPercent, m.lastMemPercent
		sb.WriteString(m.spinner.render(cpu, mem))
	}
	return sb.String()
}

// renderMarkdownAsync dispatches an asynchronous command to render markdown text.
// It returns a tea.Cmd that will yield an mdRenderCompleteMsg with the rendered text
// and the current generation.
func (m *model) renderMarkdownAsync(text string, width int, generation int) tea.Cmd {
	isDark := m.isDark // capture locally for goroutine
	return func() tea.Msg {
		style := "light"
		if isDark {
			style = "dark"
		}
		opts := []glamour.TermRendererOption{glamour.WithStandardStyle(style)}
		if width > 0 {
			opts = append(opts, glamour.WithWordWrap(width))
		}
		tr, err := glamour.NewTermRenderer(opts...)
		if err != nil {
			return mdRenderCompleteMsg{generation: generation, rendered: text}
		}
		out, err := tr.Render(text)
		if err != nil {
			return mdRenderCompleteMsg{generation: generation, rendered: text}
		}
		return mdRenderCompleteMsg{
			generation: generation,
			rendered:   strings.TrimRight(out, "\n"),
		}
	}
}
