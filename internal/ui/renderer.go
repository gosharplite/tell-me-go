// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"golang.org/x/term"
)

// TerminalLocker defines the interface for locking the terminal to prevent interleaved output.
type TerminalLocker interface {
	TerminalLock()
	TerminalUnlock()
}

// sanitizeForTerminal converts common LaTeX/Math notation that LLMs use into terminal-friendly Unicode.
func sanitizeForTerminal(text string) string {
	replacements := map[string]string{
		"$\\leftrightarrow$": "↔",
		"$\\rightarrow$":     "→",
		"$\\leftarrow$":      "←",
		"$\\Rightarrow$":     "⇒",
		"$\\Leftarrow$":      "⇐",
		"$\\dots$":           "...",
		"$\\cdot$":           "·",
		"$\\times$":          "×",
		"$\\checkmark$":      "✓",
	}
	for old, new := range replacements {
		text = strings.ReplaceAll(text, old, new)
	}
	return text
}

// UIRenderer defines the interface for UI feedback.
type UIRenderer interface {
	RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool)
	StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content)
	LogTurnStatus(status events.TurnStatus)
	LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time)
	LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool)
	LogToolResult(name string, result tools.ToolResult, showTools bool)
	LogSystemMessage(msg string, level string)
	SetUseColor(use bool)
}

// StdUIRenderer implements UIRenderer using standard output/error and Glamour.
type StdUIRenderer struct {
	locker   TerminalLocker
	stdout   io.Writer
	stderr   io.Writer
	now      func() time.Time
	renderer *glamour.TermRenderer
	mu       sync.RWMutex
	useColor bool
}

// streamState holds the transient state for a single response stream.
type streamState struct {
	aggregated      *llm.Content
	totalText       strings.Builder
	thoughtActive   bool
	showThoughts    bool
	rawOutput       bool
	lineCount       int
	hasScrolled     bool
	scrollThreshold int
}

// NewStdUIRenderer creates a new StdUIRenderer.
func NewStdUIRenderer(locker TerminalLocker) *StdUIRenderer {
	tr, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)
	r := &StdUIRenderer{
		locker:   locker,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		now:      time.Now,
		renderer: tr,
		useColor: true,
	}
	if err != nil {
		// Fallback: the renderer will be nil, and we'll handle it in renderMarkdown
		r.LogSystemMessage(fmt.Sprintf("failed to initialize glamour renderer: %v", err), "warn")
	}
	return r
}

// SetUseColor enables or disables ANSI color output.
func (r *StdUIRenderer) SetUseColor(use bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.useColor = use
}

// SetWriters allows overriding the output writers (primarily for testing).
func (r *StdUIRenderer) SetWriters(stdout, stderr io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout = stdout
	r.stderr = stderr
}

// SetNow allows overriding the time function (primarily for testing).
func (r *StdUIRenderer) SetNow(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

func (r *StdUIRenderer) getTimestamp() string {
	return r.getUIState().getTimestamp()
}

func (r *StdUIRenderer) nowSafe() time.Time {
	ui := r.getUIState()
	n := ui.now
	if n != nil {
		return n()
	}
	return time.Now()
}

func (r *StdUIRenderer) renderMarkdown(text string) {
	r.renderMarkdownWithUI(r.getUIState(), text)
}

func (r *StdUIRenderer) renderMarkdownWithUI(ui uiState, text string) {
	stdout := ui.stdout
	fmt.Fprintf(stdout, "%s────────────────────────────────────────────────────────────────────────────────%s\n", ui.c(ColorGray), ui.c(ColorReset))
	if r.renderer == nil {
		fmt.Fprint(stdout, text)
		return
	}
	out, err := r.renderer.Render(text)
	if err != nil {
		fmt.Fprint(stdout, text)
	} else {
		fmt.Fprint(stdout, out)
	}
}

type uiState struct {
	stdout   io.Writer
	stderr   io.Writer
	useColor bool
	now      func() time.Time
}

func (s uiState) c(color string) string {
	if !s.useColor {
		return ""
	}
	return color
}

func (s uiState) getTimestamp() string {
	n := s.now
	if n == nil {
		n = time.Now
	}
	return n().Format("15:04:05")
}

func (r *StdUIRenderer) getUIState() uiState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stdout := r.stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := r.stderr
	if stderr == nil {
		stderr = io.Discard
	}
	return uiState{
		stdout:   stdout,
		stderr:   stderr,
		useColor: r.useColor,
		now:      r.now,
	}
}

func (r *StdUIRenderer) LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time) {
	if logFile == "" || m == nil {
		return
	}

	m.Timestamp = r.nowSafe().Format(time.RFC3339)

	data, err := json.Marshal(m)
	if err != nil {
		return
	}

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
	_, _ = f.WriteString("\n")

	// If it's a summary (background task), print the line to terminal
	if m.IsSummary {
		if r.locker != nil {
			r.locker.TerminalLock()
			defer r.locker.TerminalUnlock()
		}
		ui := r.getUIState()
		r.renderMetricsLine(ui, m, startTime)
	}
}

func (r *StdUIRenderer) renderMetricsLine(ui uiState, m *llm.Metrics, startTime time.Time) {
	if m == nil {
		return
	}
	timestamp := ui.getTimestamp()
	stderr := ui.stderr

	miss := m.PromptTokens - m.CachedTokens

	hColor := ColorGray
	if miss > m.CachedTokens {
		hColor = ColorReset
	}

	durationStr := fmt.Sprintf("%.2fs", m.Duration)
	if m.ToolDuration > 3.0 {
		durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
	}

	timingStr := fmt.Sprintf("%s%s%s", ui.c(ColorReset), durationStr, ui.c(ColorGray))
	if !startTime.IsZero() {
		n := ui.now
		if n == nil {
			n = time.Now
		}
		totalDuration := n().Sub(startTime).Seconds()
		timingStr = fmt.Sprintf("%s%s%s / %.2fs%s", ui.c(ColorReset), durationStr, ui.c(ColorGray), totalDuration, ui.c(ColorGray))
	}

	// Prepare cost string
	costStr := ""
	if m.Cost > 0 {
		costStr = fmt.Sprintf(" %s($%.4f)%s", ui.c(ColorGray), m.Cost, ui.c(ColorGray))
	}

	fmt.Fprintf(stderr, "%s[%s] M: %d %sH: %d%s C: %d Th: %d%s %s[%s]%s\n",
		ui.c(ColorGray), timestamp, miss, ui.c(hColor), m.CachedTokens, ui.c(ColorGray), m.ResponseTokens, m.ThinkingTokens, costStr, ui.c(ColorGray), timingStr, ui.c(ColorReset))
}

func (r *StdUIRenderer) LogTurnStatus(status events.TurnStatus) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()
	timestamp := status.Timestamp.Format("15:04:05")
	stderr := ui.stderr

	printSystemLine := func(tks int, isActual bool) {
		tokenColor := ColorReset
		if float64(tks) > float64(status.MaxHistoryTokens)*config.WarningRatio {
			tokenColor = ColorYellow // Yellow caution
		}
		if float64(tks) > float64(status.MaxHistoryTokens) {
			tokenColor = ColorRed // Red limit
		}

		if isActual {
			fmt.Fprintf(stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				ui.c(ColorGray), timestamp, ui.c(tokenColor), tks, ui.c(ColorGray), status.MaxHistoryTokens, ui.c(ColorReset))
		} else {
			fmt.Fprintf(stderr, "%s[%s] Payload: ~%s%d%s/%d tokens%s\n",
				ui.c(ColorGray), timestamp, ui.c(tokenColor), tks, ui.c(ColorGray), status.MaxHistoryTokens, ui.c(ColorReset))
		}
	}

	if !status.IsPostCall {
		fmt.Fprintf(stderr, "\n%s────────────────────────────────────────────────────────────────────────────────%s\n", ui.c(ColorGray), ui.c(ColorReset))
		fmt.Fprintf(stderr, "%s╭─⠿ %sSession: %d/%d turns%s\n", ui.c(ColorGray), ui.c(ColorReset), status.SessionTurns+1, status.MaxHistoryTurns, ui.c(ColorGray))
		printSystemLine(status.Tokens, false)
		fmt.Fprintln(stderr) // Ensure visual gap before response
	} else if status.Metrics != nil {
		m := status.Metrics
		fmt.Fprintln(stderr) // Add vertical separation
		printSystemLine(int(m.PromptTokens), true)

		r.renderMetricsLine(ui, m, status.StartTime)

		costStr := ""
		if status.SessionCost > 0 {
			hitRate := 0.0
			if total := status.TotalM + status.TotalH; total > 0 {
				hitRate = float64(status.TotalH) / float64(total) * 100
			}
			// Format: (TurnCost TaskCost SessionCost DailyCost M: ... H: ... O: ...)
			// Highlight ONLY the SessionCost ($1.4745 in user example).
			costStr = fmt.Sprintf(" %s($%.4f $%.4f %s$%.4f %s$%.4f%s M: %d H: %d %.1f%% O: %d)%s",
				ui.c(ColorGray),
				status.Metrics.Cost, status.TaskCost,
				ui.c(ColorGreen), status.SessionCost,
				ui.c(ColorGray), status.DailyCost,
				ui.c(ColorGray),
				status.TotalM,
				status.TotalH,
				hitRate,
				status.TotalO,
				ui.c(ColorGray))
		}
		fmt.Fprintf(stderr, "%s╰─⠿ %sReady%s\n", ui.c(ColorGray), ui.c(ColorReset), costStr)
	}
}

func (r *StdUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()

	for _, part := range respContent.Parts {
		r.renderThought(ui, part, showThoughts)
	}
	for _, part := range respContent.Parts {
		r.renderText(ui, part, rawOutput)
		r.renderInlineData(ui, part)
	}
}

func (r *StdUIRenderer) renderThought(ui uiState, part *llm.Part, showThoughts bool) {
	if showThoughts && part.Thought && part.Text != "" {
		ts := ui.getTimestamp()
		stderr := ui.stderr
		sanitized := sanitizeForTerminal(part.Text)
		fmt.Fprintf(stderr, "%s[%s] [Thinking]\n%s%s\n", ui.c(ColorGray), ts, sanitized, ui.c(ColorReset))
	}
}

func (r *StdUIRenderer) renderText(ui uiState, part *llm.Part, raw bool) {
	if part.Text != "" && !part.Thought {
		stdout := ui.stdout
		if raw {
			fmt.Fprint(stdout, part.Text)
			if !strings.HasSuffix(part.Text, "\n") {
				fmt.Fprintln(stdout)
			}
		} else {
			sanitized := sanitizeForTerminal(part.Text)
			r.renderMarkdownWithUI(ui, sanitized)
		}
	}
}

func (r *StdUIRenderer) renderInlineData(ui uiState, part *llm.Part) {
	if part.InlineData != nil {
		ts := ui.getTimestamp()
		stderr := ui.stderr
		fmt.Fprintf(stderr, "%s[%s] [Media] %s (%d bytes)%s\n",
			ui.c(ColorGray), ts, part.InlineData.MIMEType, len(part.InlineData.Data), ui.c(ColorReset))
	}
}

func (r *StdUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
	ch := make(chan *llm.Content, 100)
	ui := r.getUIState()

	threshold := 25
	if f, ok := ui.stdout.(*os.File); ok {
		if term.IsTerminal(int(f.Fd())) {
			if _, h, err := term.GetSize(int(f.Fd())); err == nil && h > 0 {
				threshold = h - 2
			}
		}
	}

	state := &streamState{
		aggregated:      &llm.Content{Role: "model"},
		showThoughts:    showThoughts,
		rawOutput:       rawOutput,
		scrollThreshold: threshold,
	}

	if !rawOutput && ui.c(TermSaveCursor) != "" {
		if r.locker != nil {
			r.locker.TerminalLock()
		}
		fmt.Fprint(ui.stdout, TermSaveCursor)
		if r.locker != nil {
			r.locker.TerminalUnlock()
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		r.processStream(ctx, ch, state, ui)
	}()

	var once sync.Once
	finalize := func() *llm.Content {
		once.Do(func() {
			close(ch)
			wg.Wait()
			r.finalizeOutput(state, ui)
		})
		return state.aggregated
	}

	return ch, finalize
}

func (r *StdUIRenderer) processStream(ctx context.Context, ch <-chan *llm.Content, state *streamState, ui uiState) {
	for {
		select {
		case <-ctx.Done():
			return
		case content, ok := <-ch:
			if !ok {
				r.closeThinking(state, ui)
				return
			}
			for _, part := range content.Parts {
				state.aggregated.AddPart(part)
				r.renderStreamPart(state, part, ui)
			}
		}
	}
}

func (r *StdUIRenderer) renderStreamPart(state *streamState, part *llm.Part, ui uiState) {
	if part.Thought {
		r.handleThoughtPart(state, part, ui)
	} else if part.Text != "" {
		r.handleTextPart(state, part, ui)
	}

	if part.InlineData != nil {
		r.handleInlineDataPart(state, part, ui)
	}
}

func (r *StdUIRenderer) handleThoughtPart(state *streamState, part *llm.Part, ui uiState) {
	if !state.thoughtActive && state.showThoughts {
		r.safePrintStderr(fmt.Sprintf("%s[%s] [Thinking]\n", ui.c(ColorGray), ui.getTimestamp()), ui)
		state.thoughtActive = true
	}
	if state.showThoughts {
		sanitized := sanitizeForTerminal(part.Text)
		r.safePrintStderr(sanitized, ui)
	}
}

func (r *StdUIRenderer) handleTextPart(state *streamState, part *llm.Part, ui uiState) {
	r.closeThinking(state, ui)
	output := part.Text
	if !state.rawOutput {
		output = sanitizeForTerminal(part.Text)
	}

	if r.locker != nil {
		r.locker.TerminalLock()
	}
	fmt.Fprint(ui.stdout, output)
	if r.locker != nil {
		r.locker.TerminalUnlock()
	}

	// Track scrolling: If we exceed the threshold (based on terminal height),
	// we assume the terminal has scrolled, making the saved cursor position invalid.
	state.lineCount += strings.Count(part.Text, "\n")
	if state.lineCount > state.scrollThreshold {
		state.hasScrolled = true
	}

	state.totalText.WriteString(part.Text)
}

func (r *StdUIRenderer) handleInlineDataPart(state *streamState, part *llm.Part, ui uiState) {
	r.closeThinking(state, ui)
	r.safePrintStderr(fmt.Sprintf("\n%s[%s] [Media] %s (%d bytes)%s\n",
		ui.c(ColorGray), ui.getTimestamp(), part.InlineData.MIMEType, len(part.InlineData.Data), ui.c(ColorReset)), ui)
}

func (r *StdUIRenderer) closeThinking(state *streamState, ui uiState) {
	if state.thoughtActive {
		r.safePrintStderr(ui.c(ColorReset)+"\n", ui)
		state.thoughtActive = false
	}
}

func (r *StdUIRenderer) safePrintStderr(msg string, ui uiState) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	fmt.Fprint(ui.stderr, msg)
}

func (r *StdUIRenderer) finalizeOutput(state *streamState, ui uiState) {
	if !state.rawOutput {
		fullText := state.totalText.String()
		if fullText != "" {
			sanitized := sanitizeForTerminal(fullText)

			if state.hasScrolled {
				// FAIL-SAFE: Terminal scrolled. Redrawing would cause overlap.
				// Just print a separator and append the final formatted text.
				r.safePrintStderr("\n"+ui.c(ColorGray)+"── (formatted) ──"+ui.c(ColorReset)+"\n", ui)
				r.renderMarkdownWithUI(ui, sanitized)
			} else {
				// Normal path: Cursor is still valid, do a clean redraw.
				r.clearAndRenderMarkdown(ui, sanitized)
			}
		}
		if r.locker != nil {
			r.locker.TerminalLock()
		}
		fmt.Fprintln(ui.stdout)
		if r.locker != nil {
			r.locker.TerminalUnlock()
		}
	}
}

func (r *StdUIRenderer) clearAndRenderMarkdown(ui uiState, fullText string) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	fmt.Fprint(ui.stdout, ui.c(TermRestoreCursor)+ui.c(TermClearForward))
	r.renderMarkdownWithUI(ui, fullText)
}

func (r *StdUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	ui := r.getUIState()
	stderr := ui.stderr

	ts := ui.getTimestamp()
	var names []string
	for _, fc := range calls {
		names = append(names, fc.Name)
	}

	fmt.Fprintf(stderr, "%s[%s] [Tool Engine (Step %d/%d)] Calling: %s%s\n",
		ui.c(ColorCyan), ts, turn+1, maxTurns, strings.Join(names, ", "), ui.c(ColorReset))

	if showTools {
		for _, fc := range calls {
			var argParts []string
			for k, v := range fc.Args {
				valStr := fmt.Sprintf("%v", v)
				if len(valStr) > 60 {
					valStr = valStr[:57] + "..."
				}
				argParts = append(argParts, fmt.Sprintf("%s: %v", k, valStr))
			}
			fmt.Fprintf(stderr, "%s[%s] [Tool Action] %s(%s)%s\n",
				ui.c(ColorMagenta), ts, fc.Name, strings.Join(argParts, ", "), ui.c(ColorReset))
		}
	}
}

func (r *StdUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	if !showTools {
		return
	}

	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	ui := r.getUIState()
	stderr := ui.stderr

	timestamp := ui.getTimestamp()

	if result.Text != "" {
		snippet := result.Text
		if len(snippet) > 200 {
			snippet = snippet[:197] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: %s%s\n", ui.c(ColorCyan), timestamp, name, snippet, ui.c(ColorReset))
	}

	for _, b := range result.BinaryData {
		fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
			ui.c(ColorCyan), timestamp, name, b.MIMEType, len(b.Data), ui.c(ColorReset))
	}

	if m, ok := result.Metadata["metrics"].(*llm.Metrics); ok {
		r.renderMetricsLine(ui, m, time.Time{}) // Render the usage line after the result
	}
}

func (r *StdUIRenderer) LogSystemMessage(msg string, level string) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	ui := r.getUIState()
	stderr := ui.stderr

	color := ColorGray
	prefix := "System"

	switch level {
	case "error":
		color = ColorRed
		prefix = "Error"
	case "warn":
		color = ColorGray
		prefix = "Warning"
	case "info":
		color = ColorCyan
		prefix = "Info"
	}

	fmt.Fprintf(stderr, "%s[%s] [%s] %s%s\n",
		ui.c(color), ui.getTimestamp(), prefix, msg, ui.c(ColorReset))
}
