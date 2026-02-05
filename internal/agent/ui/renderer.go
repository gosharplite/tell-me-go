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
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
)

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
}

// StdUIRenderer implements UIRenderer using standard output/error and Glamour.
type StdUIRenderer struct {
	sm       *security.SecurityManager
	stdout   io.Writer
	stderr   io.Writer
	now      func() time.Time
	renderer *glamour.TermRenderer
	mu       sync.RWMutex
}

// streamState holds the transient state for a single response stream.
type streamState struct {
	aggregated    *llm.Content
	totalText     strings.Builder
	thoughtActive bool
	showThoughts  bool
	rawOutput     bool
	lineCount     int
	hasScrolled   bool
}

// NewStdUIRenderer creates a new StdUIRenderer.
func NewStdUIRenderer(sm *security.SecurityManager) *StdUIRenderer {
	tr, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)
	r := &StdUIRenderer{
		sm:       sm,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		now:      time.Now,
		renderer: tr,
	}
	if err != nil {
		// Fallback: the renderer will be nil, and we'll handle it in renderMarkdown
		r.LogSystemMessage(fmt.Sprintf("failed to initialize glamour renderer: %v", err), "warn")
	}
	return r
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
	return r.nowSafe().Format("15:04:05")
}

func (r *StdUIRenderer) nowSafe() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := r.now
	if n != nil {
		return n()
	}
	return time.Now()
}

func (r *StdUIRenderer) getStdout() io.Writer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stdout
}

func (r *StdUIRenderer) getStderr() io.Writer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stderr
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
		r.renderMetricsLine(m, startTime)
	}
}

func (r *StdUIRenderer) renderMetricsLine(m *llm.Metrics, startTime time.Time) {
	if m == nil {
		return
	}
	timestamp := r.getTimestamp()
	stderr := r.getStderr()

	miss := m.PromptTokens - m.CachedTokens

	hColor := colors.ColorGray
	if miss > m.CachedTokens {
		hColor = colors.ColorReset
	}

	durationStr := fmt.Sprintf("%.2fs", m.Duration)
	if m.ToolDuration > 3.0 {
		durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
	}

	timingStr := fmt.Sprintf("%s%s%s", colors.ColorReset, durationStr, colors.ColorGray)
	if !startTime.IsZero() {
		totalDuration := r.nowSafe().Sub(startTime).Seconds()
		timingStr = fmt.Sprintf("%s%s%s / %.2fs%s", colors.ColorReset, durationStr, colors.ColorGray, totalDuration, colors.ColorGray)
	}

	// Prepare cost string
	costStr := ""
	if m.Cost > 0 {
		costStr = fmt.Sprintf(" %s($%.4f)%s", colors.ColorGray, m.Cost, colors.ColorGray)
	}

	fmt.Fprintf(stderr, "%s[%s] M: %d %sH: %d%s C: %d Th: %d%s %s[%s]%s\n",
		colors.ColorGray, timestamp, miss, hColor, m.CachedTokens, colors.ColorGray, m.ResponseTokens, m.ThinkingTokens, costStr, colors.ColorGray, timingStr, colors.ColorReset)
}

func (r *StdUIRenderer) LogTurnStatus(status events.TurnStatus) {
	timestamp := status.Timestamp.Format("15:04:05")
	stderr := r.getStderr()

	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	printSystemLine := func(tks int, isActual bool) {
		tokenColor := colors.ColorReset
		if float64(tks) > float64(status.MaxHistoryTokens)*config.WarningRatio {
			tokenColor = colors.ColorYellow // Yellow caution
		}
		if float64(tks) > float64(status.MaxHistoryTokens) {
			tokenColor = colors.ColorRed // Red limit
		}

		if isActual {
			fmt.Fprintf(stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				colors.ColorGray, timestamp, tokenColor, tks, colors.ColorGray, status.MaxHistoryTokens, colors.ColorReset)
		} else {
			fmt.Fprintf(stderr, "%s[%s] Payload: ~%s%d%s/%d tokens%s\n",
				colors.ColorGray, timestamp, tokenColor, tks, colors.ColorGray, status.MaxHistoryTokens, colors.ColorReset)
		}
	}

	if !status.IsPostCall {
		fmt.Fprintf(stderr, "\n%s────────────────────────────────────────────────────────────────────────────────%s\n", colors.ColorGray, colors.ColorReset)
		fmt.Fprintf(stderr, "%s╭─⠿ %sSession: %d/%d turns%s\n", colors.ColorGray, colors.ColorReset, status.SessionTurns+1, status.MaxHistoryTurns, colors.ColorGray)
		printSystemLine(status.Tokens, false)
		fmt.Fprintln(stderr) // Ensure visual gap before response
	} else if status.Metrics != nil {
		m := status.Metrics
		fmt.Fprintln(stderr) // Add vertical separation
		printSystemLine(int(m.PromptTokens), true)

		r.renderMetricsLine(m, status.StartTime)

		costStr := ""
		if status.SessionCost > 0 {
			hitRate := 0.0
			if total := status.TotalM + status.TotalH; total > 0 {
				hitRate = float64(status.TotalH) / float64(total) * 100
			}
			// Format: (TurnCost TaskCost SessionCost M: ... H: ... O: ...)
			// Highlight ONLY the SessionCost ($1.4745 in user example).
			costStr = fmt.Sprintf(" %s($%.4f $%.4f %s$%.4f%s M: %d H: %d %.1f%% O: %d)%s",
				colors.ColorGray,
				status.Metrics.Cost, status.TaskCost,
				colors.ColorGreen, status.SessionCost, colors.ColorGray,
				status.TotalM,
				status.TotalH,
				hitRate,
				status.TotalO,
				colors.ColorGray)
		}
		fmt.Fprintf(stderr, "%s╰─⠿ %sReady%s\n", colors.ColorGray, colors.ColorReset, costStr)
	}
}

func (r *StdUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	ts := r.getTimestamp()
	stdout := r.getStdout()
	stderr := r.getStderr()

	for _, part := range respContent.Parts {
		if showThoughts && part.Thought && part.Text != "" {
			sanitized := sanitizeForTerminal(part.Text)
			fmt.Fprintf(stderr, "%s[%s] [Thinking]\n%s%s\n", colors.ColorGray, ts, sanitized, colors.ColorReset)
		}
	}
	for _, part := range respContent.Parts {
		if part.Text != "" && !part.Thought {
			if rawOutput {
				fmt.Fprint(stdout, part.Text)
				if !strings.HasSuffix(part.Text, "\n") {
					fmt.Fprintln(stdout)
				}
			} else {
				sanitized := sanitizeForTerminal(part.Text)
				r.renderMarkdown(sanitized)
			}
		}

		if part.InlineData != nil {
			fmt.Fprintf(stderr, "%s[%s] [Media] %s (%d bytes)%s\n",
				colors.ColorGray, ts, part.InlineData.MIMEType, len(part.InlineData.Data), colors.ColorReset)
		}
	}
}

func (r *StdUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
	ch := make(chan *llm.Content, 100)
	state := &streamState{
		aggregated:   &llm.Content{Role: "model"},
		showThoughts: showThoughts,
		rawOutput:    rawOutput,
	}

	if !rawOutput {
		stdout := r.getStdout()
		func() {
			r.sm.TerminalLock()
			defer r.sm.TerminalUnlock()
			fmt.Fprint(stdout, colors.TermSaveCursor)
		}()
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		r.processStream(ctx, ch, state)
	}()

	var once sync.Once
	finalize := func() *llm.Content {
		once.Do(func() {
			close(ch)
			wg.Wait()
			r.finalizeOutput(state)
		})
		return state.aggregated
	}

	return ch, finalize
}

func (r *StdUIRenderer) processStream(ctx context.Context, ch <-chan *llm.Content, state *streamState) {
	for {
		select {
		case <-ctx.Done():
			return
		case content, ok := <-ch:
			if !ok {
				r.closeThinking(state)
				return
			}
			for _, part := range content.Parts {
				state.aggregated.AddPart(part)
				r.renderStreamPart(state, part)
			}
		}
	}
}

func (r *StdUIRenderer) renderStreamPart(state *streamState, part *llm.Part) {
	if part.Thought {
		r.handleThoughtPart(state, part)
	} else if part.Text != "" {
		r.handleTextPart(state, part)
	}

	if part.InlineData != nil {
		r.handleInlineDataPart(state, part)
	}
}

func (r *StdUIRenderer) handleThoughtPart(state *streamState, part *llm.Part) {
	if !state.thoughtActive && state.showThoughts {
		r.safePrintStderr(fmt.Sprintf("%s[%s] [Thinking]\n", colors.ColorGray, r.getTimestamp()))
		state.thoughtActive = true
	}
	if state.showThoughts {
		sanitized := sanitizeForTerminal(part.Text)
		r.safePrintStderr(sanitized)
	}
}

func (r *StdUIRenderer) handleTextPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	output := part.Text
	if !state.rawOutput {
		output = sanitizeForTerminal(part.Text)
	}
	stdout := r.getStdout()
	func() {
		r.sm.TerminalLock()
		defer r.sm.TerminalUnlock()
		fmt.Fprint(stdout, output)
	}()

	// Track scrolling: If we exceed a reasonable line count, we assume the terminal
	// has scrolled, making the saved cursor position for redraws invalid.
	state.lineCount += strings.Count(part.Text, "\n")
	if state.lineCount > 25 {
		state.hasScrolled = true
	}

	state.totalText.WriteString(part.Text)
}

func (r *StdUIRenderer) handleInlineDataPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	r.safePrintStderr(fmt.Sprintf("\n%s[%s] [Media] %s (%d bytes)%s\n",
		colors.ColorGray, r.getTimestamp(), part.InlineData.MIMEType, len(part.InlineData.Data), colors.ColorReset))
}

func (r *StdUIRenderer) closeThinking(state *streamState) {
	if state.thoughtActive {
		r.safePrintStderr(colors.ColorReset + "\n")
		state.thoughtActive = false
	}
}

func (r *StdUIRenderer) safePrintStderr(msg string) {
	stderr := r.getStderr()
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()
	fmt.Fprint(stderr, msg)
}

func (r *StdUIRenderer) finalizeOutput(state *streamState) {
	if !state.rawOutput {
		fullText := state.totalText.String()
		if fullText != "" {
			sanitized := sanitizeForTerminal(fullText)

			if state.hasScrolled {
				// FAIL-SAFE: Terminal scrolled. Redrawing would cause overlap.
				// Just print a separator and append the final formatted text.
				r.safePrintStderr("\n" + colors.ColorGray + "── (formatted) ──" + colors.ColorReset + "\n")
				r.renderMarkdown(sanitized)
			} else {
				// Normal path: Cursor is still valid, do a clean redraw.
				r.clearAndRenderMarkdown(sanitized)
			}
		}
		stdout := r.getStdout()
		func() {
			r.sm.TerminalLock()
			defer r.sm.TerminalUnlock()
			fmt.Fprintln(stdout)
		}()
	}
}

func (r *StdUIRenderer) clearAndRenderMarkdown(fullText string) {
	stdout := r.getStdout()
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()
	fmt.Fprint(stdout, colors.TermRestoreCursor+colors.TermClearForward)
	r.renderMarkdown(fullText)
}

func (r *StdUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	stderr := r.getStderr()
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	ts := r.getTimestamp()
	var names []string
	for _, fc := range calls {
		names = append(names, fc.Name)
	}

	fmt.Fprintf(stderr, "%s[%s] [Tool Engine (Step %d/%d)] Calling: %s%s\n",
		colors.ColorCyan, ts, turn+1, maxTurns, strings.Join(names, ", "), colors.ColorReset)

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
				colors.ColorCyan, ts, fc.Name, strings.Join(argParts, ", "), colors.ColorReset)
		}
	}
}

func (r *StdUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	if !showTools {
		return
	}

	stderr := r.getStderr()
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	timestamp := r.getTimestamp()

	if result.Text != "" {
		snippet := result.Text
		if len(snippet) > 200 {
			snippet = snippet[:197] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: %s%s\n", colors.ColorCyan, timestamp, name, snippet, colors.ColorReset)
	}

	for _, b := range result.BinaryData {
		fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
			colors.ColorCyan, timestamp, name, b.MIMEType, len(b.Data), colors.ColorReset)
	}

	if m, ok := result.Metadata["metrics"].(*llm.Metrics); ok {
		r.renderMetricsLine(m, time.Time{}) // Render the usage line after the result
	}
}

func (r *StdUIRenderer) LogSystemMessage(msg string, level string) {
	stderr := r.getStderr()
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	color := colors.ColorGray
	prefix := "System"

	switch level {
	case "error":
		color = colors.ColorRed
		prefix = "Error"
	case "warn":
		color = colors.ColorGray
		prefix = "Warning"
	case "info":
		color = colors.ColorCyan
		prefix = "Info"
	}

	fmt.Fprintf(stderr, "%s[%s] [%s] %s%s\n",
		color, r.getTimestamp(), prefix, msg, colors.ColorReset)
}

func (r *StdUIRenderer) renderMarkdown(text string) {
	stdout := r.getStdout()
	fmt.Fprintf(stdout, "%s────────────────────────────────────────────────────────────────────────────────%s\n", colors.ColorGray, colors.ColorReset)
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
