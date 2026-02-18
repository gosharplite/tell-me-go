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
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/term"
)

// stdUIRenderer implements services.UIRenderer using standard output/error and Glamour.
type stdUIRenderer struct {
	locker   domain_security.ISecurityManager
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

// NewRenderer creates a new services.UIRenderer.
func NewRenderer(locker domain_security.ISecurityManager, stdout, stderr io.Writer) services.UIRenderer {
	tr, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)
	r := &stdUIRenderer{
		locker:   locker,
		stdout:   stdout,
		stderr:   stderr,
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

// SetUseColor enables or disables ANSI color output.
func (r *stdUIRenderer) SetUseColor(use bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.useColor = use
}

// SetWriters allows overriding the output writers (primarily for testing).
func (r *stdUIRenderer) SetWriters(stdout, stderr io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout = stdout
	r.stderr = stderr
}

// SetNow allows overriding the time function (primarily for testing).
func (r *stdUIRenderer) SetNow(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

func (r *stdUIRenderer) getTimestamp() string {
	return r.getUIState().getTimestamp()
}

func (r *stdUIRenderer) nowSafe() time.Time {
	ui := r.getUIState()
	n := ui.now
	if n != nil {
		return n()
	}
	return time.Now()
}

func (r *stdUIRenderer) renderMarkdown(text string) {
	r.renderMarkdownWithUI(r.getUIState(), text)
}

func (r *stdUIRenderer) renderMarkdownWithUI(ui uiState, text string) {
	stdout := ui.stdout
	fmt.Fprintf(stdout, "%s────────────────────────────────────────────────────────────────────────────────%s\n", ui.c(colorGray), ui.c(colorReset))
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

func (r *stdUIRenderer) getUIState() uiState {
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

func (r *stdUIRenderer) LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time) {
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

func (r *stdUIRenderer) renderMetricsLine(ui uiState, m *llm.Metrics, startTime time.Time) {
	if m == nil {
		return
	}
	timestamp := ui.getTimestamp()
	stderr := ui.stderr

	miss := m.PromptTokens - m.CachedTokens

	hColor := colorGray
	if miss > m.CachedTokens {
		hColor = colorReset
	}

	modelStr := ""
	displayName := m.Provider
	if displayName == "" {
		displayName = m.Model
	}
	if displayName != "" {
		modelStr = fmt.Sprintf(" [%s]", displayName)
	}

	totalTurnLatency := m.Duration + m.ToolDuration
	timingStr := fmt.Sprintf("%s%.2fs %s(ΣT: %.2fs)%s",
		ui.c(colorReset), totalTurnLatency,
		ui.c(colorGray), m.CumulativeToolDuration,
		ui.c(colorGray))

	if !startTime.IsZero() {
		n := ui.now
		if n == nil {
			n = time.Now
		}
		totalSessionDuration := n().Sub(startTime).Seconds()
		timingStr = fmt.Sprintf("%s / %.2fs%s", timingStr, totalSessionDuration, ui.c(colorGray))
	}

	// Prepare cost string
	costStr := ""
	if m.Cost > 0 {
		costStr = fmt.Sprintf(" %s($%.4f)%s", ui.c(colorGray), m.Cost, ui.c(colorGray))
	}

	fmt.Fprintf(stderr, "%s[%s]%s M: %d %sH: %d%s C: %d Th: %d%s %s[%s]%s\n",
		ui.c(colorGray), timestamp, modelStr, miss, ui.c(hColor), m.CachedTokens, ui.c(colorGray), m.ResponseTokens, m.ThinkingTokens, costStr, ui.c(colorGray), timingStr, ui.c(colorReset))
}

func (r *stdUIRenderer) LogTurnStatus(status events.TurnStatus) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()
	timestamp := status.Timestamp.Format("15:04:05")
	stderr := ui.stderr

	printSystemLine := func(tks int, isActual bool) {
		tokenColor := colorReset
		if float64(tks) > float64(status.MaxHistoryTokens)*config.WarningRatio {
			tokenColor = colorYellow // Yellow caution
		}
		if float64(tks) > float64(status.MaxHistoryTokens) {
			tokenColor = colorRed // Red limit
		}

		if isActual {
			fmt.Fprintf(stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				ui.c(colorGray), timestamp, ui.c(tokenColor), tks, ui.c(colorGray), status.MaxHistoryTokens, ui.c(colorReset))
		} else {
			fmt.Fprintf(stderr, "%s[%s] Payload: ~%s%d%s/%d tokens%s\n",
				ui.c(colorGray), timestamp, ui.c(tokenColor), tks, ui.c(colorGray), status.MaxHistoryTokens, ui.c(colorReset))
		}
	}

	if !status.IsPostCall {
		fmt.Fprintf(stderr, "\n%s────────────────────────────────────────────────────────────────────────────────%s\n", ui.c(colorGray), ui.c(colorReset))
		fmt.Fprintf(stderr, "%s╭─⠿ %sSession: %d/%d turns%s\n", ui.c(colorGray), ui.c(colorReset), status.SessionTurns+1, status.MaxHistoryTurns, ui.c(colorGray))
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
				ui.c(colorGray),
				status.Metrics.Cost, status.TaskCost,
				ui.c(colorGreen), status.SessionCost,
				ui.c(colorGray), status.DailyCost,
				ui.c(colorGray),
				status.TotalM,
				status.TotalH,
				hitRate,
				status.TotalO,
				ui.c(colorGray))
		}
		fmt.Fprintf(stderr, "%s╰─⠿ %sReady%s\n", ui.c(colorGray), ui.c(colorReset), costStr)
	}
}

func (r *stdUIRenderer) renderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()

	for _, part := range respContent.Parts {
		r.renderThought(ui, part, showThoughts)
		r.renderText(ui, part, rawOutput)
		r.renderInlineData(ui, part)
	}
}

func (r *stdUIRenderer) renderThought(ui uiState, part *llm.Part, showThoughts bool) {
	if showThoughts && (part.IsThought || len(part.ThoughtSignature) > 0) {
		ts := ui.getTimestamp()
		stderr := ui.stderr

		if part.Text == "" {
			return
		}

		sanitized := sanitizeForTerminal(part.Text)
		fmt.Fprintf(stderr, "%s[%s] [Thinking]\n%s%s\n", ui.c(colorGray), ts, sanitized, ui.c(colorReset))
	}
}

func (r *stdUIRenderer) renderText(ui uiState, part *llm.Part, raw bool) {
	if part.Text != "" && !part.IsThought {
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

func (r *stdUIRenderer) renderInlineData(ui uiState, part *llm.Part) {
	if part.InlineData != nil {
		ts := ui.getTimestamp()
		stderr := ui.stderr
		fmt.Fprintf(stderr, "%s[%s] [Media] %s (%d bytes)%s\n",
			ui.c(colorGray), ts, part.InlineData.MIMEType, len(part.InlineData.Data), ui.c(colorReset))
	}
}

func (r *stdUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
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

	if !rawOutput && ui.c(termSaveCursor) != "" {
		if r.locker != nil {
			r.locker.TerminalLock()
		}
		fmt.Fprint(ui.stdout, termSaveCursor)
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

func (r *stdUIRenderer) processStream(ctx context.Context, ch <-chan *llm.Content, state *streamState, ui uiState) {
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

func (r *stdUIRenderer) renderStreamPart(state *streamState, part *llm.Part, ui uiState) {
	if part.IsThought || len(part.ThoughtSignature) > 0 {
		r.handleThoughtPart(state, part, ui)
	}
	if part.Text != "" {
		r.handleTextPart(state, part, ui)
	}

	if part.InlineData != nil {
		r.handleInlineDataPart(state, part, ui)
	}
}

func (r *stdUIRenderer) handleThoughtPart(state *streamState, part *llm.Part, ui uiState) {
	if !part.IsThought && len(part.ThoughtSignature) == 0 {
		return
	}
	if !state.thoughtActive && state.showThoughts {
		r.safePrintStderr(fmt.Sprintf("%s[%s] [Thinking]\n", ui.c(colorGray), ui.getTimestamp()), ui)
		state.thoughtActive = true
	}
	if state.showThoughts && part.Text != "" {
		sanitized := sanitizeForTerminal(part.Text)
		r.safePrintStderr(sanitized, ui)
	}
}

func (r *stdUIRenderer) handleTextPart(state *streamState, part *llm.Part, ui uiState) {
	if part.IsThought {
		return
	}
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

func (r *stdUIRenderer) handleInlineDataPart(state *streamState, part *llm.Part, ui uiState) {
	r.closeThinking(state, ui)
	r.safePrintStderr(fmt.Sprintf("\n%s[%s] [Media] %s (%d bytes)%s\n",
		ui.c(colorGray), ui.getTimestamp(), part.InlineData.MIMEType, len(part.InlineData.Data), ui.c(colorReset)), ui)
}

func (r *stdUIRenderer) closeThinking(state *streamState, ui uiState) {
	if state.thoughtActive {
		r.safePrintStderr(ui.c(colorReset)+"\n", ui)
		state.thoughtActive = false
	}
}

func (r *stdUIRenderer) safePrintStderr(msg string, ui uiState) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	fmt.Fprint(ui.stderr, msg)
}

func (r *stdUIRenderer) finalizeOutput(state *streamState, ui uiState) {
	if !state.rawOutput {
		fullText := state.totalText.String()
		if fullText != "" {
			sanitized := sanitizeForTerminal(fullText)

			if state.hasScrolled {
				// FAIL-SAFE: Terminal scrolled. Redrawing would cause overlap.
				// Just print a separator and append the final formatted text.
				r.safePrintStderr("\n"+ui.c(colorGray)+"── (formatted) ──"+ui.c(colorReset)+"\n", ui)
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

func (r *stdUIRenderer) clearAndRenderMarkdown(ui uiState, fullText string) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	fmt.Fprint(ui.stdout, ui.c(termRestoreCursor)+ui.c(termClearForward))
	r.renderMarkdownWithUI(ui, fullText)
}

func (r *stdUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
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
		ui.c(colorCyan), ts, turn+1, maxTurns, strings.Join(names, ", "), ui.c(colorReset))

	if showTools {
		for _, fc := range calls {
			// Extract and display the reason if present
			if reason, ok := fc.Args["reason"].(string); ok && reason != "" {
				fmt.Fprintf(stderr, "%s[%s] [Tool Reason] %s%s\n",
					ui.c(colorGray), ts, reason, ui.c(colorReset))
			}

			var argParts []string
			for k, v := range fc.Args {
				if k == "reason" {
					continue // Already shown as [Tool Reason]
				}
				valStr := fmt.Sprintf("%v", v)
				if len(valStr) > 60 {
					valStr = valStr[:57] + "..."
				}
				argParts = append(argParts, fmt.Sprintf("%s: %v", k, valStr))
			}
			fmt.Fprintf(stderr, "%s[%s] [Tool Action] %s(%s)%s\n",
				ui.c(colorMagenta), ts, fc.Name, strings.Join(argParts, ", "), ui.c(colorReset))
		}
	}
}

func (r *stdUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
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
		fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: %s%s\n", ui.c(colorCyan), timestamp, name, snippet, ui.c(colorReset))
	}

	for _, b := range result.BinaryData {
		fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
			ui.c(colorCyan), timestamp, name, b.MIMEType, len(b.Data), ui.c(colorReset))
	}

	if m, ok := result.Metadata["metrics"].(*llm.Metrics); ok {
		r.renderMetricsLine(ui, m, time.Time{}) // Render the usage line after the result
	}
}

func (r *stdUIRenderer) LogSystemMessage(msg string, level string) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	ui := r.getUIState()
	stderr := ui.stderr

	color := colorGray
	prefix := "System"

	switch level {
	case "error":
		color = colorRed
		prefix = "Error"
	case "warn":
		color = colorGray
		prefix = "Warning"
	case "info":
		color = colorCyan
		prefix = "Info"
	}

	fmt.Fprintf(stderr, "%s[%s] [%s] %s%s\n",
		ui.c(color), ui.getTimestamp(), prefix, msg, ui.c(colorReset))
}
