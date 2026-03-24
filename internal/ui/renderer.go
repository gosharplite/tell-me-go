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
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/term"
)

// stdUIRenderer implements ports.UIRenderer using standard output/error and Glamour.
type stdUIRenderer struct {
	locker       domain_security.Manager
	stdout       io.Writer
	stderr       io.Writer
	clock        clock.Clock
	renderer     *glamour.TermRenderer
	mu           sync.RWMutex
	useColor     bool
	forceSpinner bool
}

// NewRenderer creates a new ports.UIRenderer.
func NewRenderer(locker domain_security.Manager, stdout, stderr io.Writer, clk clock.Clock) ports.UIRenderer {
	if clk == nil {
		clk = clock.RealClock{}
	}
	tr, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)
	r := &stdUIRenderer{
		locker:   locker,
		stdout:   stdout,
		stderr:   stderr,
		clock:    clk,
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

// SetForceSpinner enables or disables forcing the spinner even in non-terminal environments (primarily for testing).
func (r *stdUIRenderer) SetForceSpinner(force bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forceSpinner = force
}

// SetWriters allows overriding the output writers (primarily for testing).
func (r *stdUIRenderer) SetWriters(stdout, stderr io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout = stdout
	r.stderr = stderr
}

// SetClock allows overriding the clock (primarily for testing).
func (r *stdUIRenderer) SetClock(clk clock.Clock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if clk == nil {
		clk = clock.RealClock{}
	}
	r.clock = clk
}

func (r *stdUIRenderer) getTimestamp() string {
	return r.getUIState().getTimestamp()
}

func (r *stdUIRenderer) nowSafe() time.Time {
	ui := r.getUIState()
	return ui.clock.Now()
}

func (r *stdUIRenderer) renderMarkdown(text string) {
	r.renderMarkdownWithUI(r.getUIState(), text)
}

func (r *stdUIRenderer) renderMarkdownWithUI(ui uiState, text string) {
	stdout := ui.stdout
	if r.renderer == nil {
		_, _ = fmt.Fprint(stdout, text)
		return
	}
	out, err := r.renderer.Render(text)
	if err != nil {
		_, _ = fmt.Fprint(stdout, text)
	} else {
		out = strings.TrimLeft(out, "\n")
		out = strings.TrimRight(out, "\n")
		if out != "" {
			_, _ = fmt.Fprint(stdout, out+"\n\n")
		}
	}
}

type uiState struct {
	stdout   io.Writer
	stderr   io.Writer
	useColor bool
	clock    clock.Clock
}

func (s uiState) c(color string) string {
	if !s.useColor {
		return ""
	}
	return color
}

func (s uiState) getTimestamp() string {
	return s.clock.Now().Format("15:04:05")
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
		clock:    r.clock,
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
	defer func() {
		_ = f.Close()
	}()
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
		totalSessionDuration := ui.clock.Now().Sub(startTime).Seconds()
		timingStr = fmt.Sprintf("%s / %.2fs%s", timingStr, totalSessionDuration, ui.c(colorGray))
	}

	// Prepare cost string
	costStr := ""
	if m.Cost > 0 {
		costStr = fmt.Sprintf(" %s($%.4f)%s", ui.c(colorGray), m.Cost, ui.c(colorGray))
	}

	_, _ = fmt.Fprintf(stderr, "%s[%s]%s M: %d %sH: %d%s C: %d Th: %d%s %s[%s]%s\n",
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
			_, _ = fmt.Fprintf(stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				ui.c(colorGray), timestamp, ui.c(tokenColor), tks, ui.c(colorGray), status.MaxHistoryTokens, ui.c(colorReset))
		} else {
			_, _ = fmt.Fprintf(stderr, "%s[%s] Payload: ~%s%d%s/%d tokens%s\n",
				ui.c(colorGray), timestamp, ui.c(tokenColor), tks, ui.c(colorGray), status.MaxHistoryTokens, ui.c(colorReset))
		}
	}

	if !status.IsPostCall && !status.IsFinal {
		_, _ = fmt.Fprintf(stderr, "\n%s────────────────────────────────────────────────────────────────────────────────%s\n", ui.c(colorGray), ui.c(colorReset))

		if status.MaxHistoryTurns > 0 {
			_, _ = fmt.Fprintf(stderr, "%s╭─⠿ %sTurn %d/%d%s\n", ui.c(colorGray), ui.c(colorReset), status.SessionTurns+1, status.MaxHistoryTurns, ui.c(colorGray))
		} else {
			_, _ = fmt.Fprintf(stderr, "%s╭─⠿ %sTurn %d%s\n", ui.c(colorGray), ui.c(colorReset), status.SessionTurns+1, ui.c(colorGray))
		}

		printSystemLine(status.Tokens, false)
		_, _ = fmt.Fprintln(stderr) // Ensure visual gap before response
	}

	if status.IsPostCall && status.Metrics != nil {
		m := status.Metrics

		if len(status.ToolReasons) > 0 {
			for _, reason := range status.ToolReasons {
				_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Reason] %s%s\n",
					ui.c(colorGray), status.Timestamp.Format("15:04:05"), reason, ui.c(colorReset))
			}
		}

		printSystemLine(int(m.PromptTokens), true)
		r.renderMetricsLine(ui, m, status.StartTime)
	}

	if status.IsFinal {
		costStr := ""
		if status.SessionCost > 0 {
			hitRate := 0.0
			if total := status.TotalM + status.TotalH; total > 0 {
				hitRate = float64(status.TotalH) / float64(total) * 100
			}

			// Safe access to metrics; metrics could be nil if the turn stopped before inference
			turnCost := 0.0
			if status.Metrics != nil {
				turnCost = status.Metrics.Cost
			}

			// Format: (TurnCost TaskCost SessionCost DailyCost M: ... H: ... O: ...)
			// Highlight ONLY the SessionCost ($1.4745 in user example).
			costStr = fmt.Sprintf(" %s($%.4f $%.4f %s$%.4f %s$%.4f%s M: %d H: %d %.1f%% O: %d)%s",
				ui.c(colorGray),
				turnCost, status.TaskCost,
				ui.c(colorGreen), status.SessionCost,
				ui.c(colorGray), status.DailyCost,
				ui.c(colorGray),
				status.TotalM,
				status.TotalH,
				hitRate,
				status.TotalO,
				ui.c(colorGray))
		}

		_, _ = fmt.Fprintf(stderr, "%s╰─⠿ %sReady%s\n", ui.c(colorGray), ui.c(colorReset), costStr)
	}
}

func (r *stdUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
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
		_, _ = fmt.Fprintf(stderr, "%s[%s] [Thinking]\n%s%s\n", ui.c(colorGray), ts, sanitized, ui.c(colorReset))
	}
}

func (r *stdUIRenderer) renderText(ui uiState, part *llm.Part, raw bool) {
	if part.Text != "" && !part.IsThought {
		stdout := ui.stdout
		if raw {
			_, _ = fmt.Fprint(stdout, part.Text)
			if !strings.HasSuffix(part.Text, "\n") {
				_, _ = fmt.Fprintln(stdout)
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
		_, _ = fmt.Fprintf(stderr, "%s[%s] [Media] %s (%d bytes)%s\n",
			ui.c(colorGray), ts, part.InlineData.MIMEType, len(part.InlineData.Data), ui.c(colorReset))
	}
}

func (r *stdUIRenderer) isTerminalContext() bool {
	ui := r.getUIState()
	if f, ok := ui.stderr.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return true
	}
	return false
}

func (r *stdUIRenderer) StartSpinner(ctx context.Context) func() {
	ui := r.getUIState()
	r.mu.RLock()
	force := r.forceSpinner
	r.mu.RUnlock()

	if !r.isTerminalContext() && !force {
		return func() {}
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	idx := 0
	startTime := r.nowSafe()
	done := make(chan struct{})

	// Draw the first frame synchronously to avoid 200ms delay.
	r.updateIndicatorFrame(ui, frames, &idx, startTime)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				r.updateIndicatorFrame(ui, frames, &idx, startTime)
			}
		}
	}()

	var stopped bool
	return func() {
		if !stopped {
			stopped = true
			ticker.Stop()
			close(done)
			r.clearLoadingIndicator(ui, false)
		}
	}
}

func (r *stdUIRenderer) drawLoadingIndicator(ui uiState, frame string, startTime time.Time) {
	msg := " Thinking..."
	if !startTime.IsZero() {
		elapsed := int(ui.clock.Now().Sub(startTime).Seconds())
		msg = fmt.Sprintf(" Thinking... (%ds)", elapsed)
	}

	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	// Move to start of line, clear current line, then print the indicator.
	_, _ = fmt.Fprintf(ui.stderr, "\r%s%s%s%s%s", ui.c(termClearLine), ui.c(colorGray), frame, msg, ui.c(colorReset))
}

func (r *stdUIRenderer) clearLoadingIndicator(ui uiState, rawOutput bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	// Move to start of line and clear the spinner.
	// We do NOT add a newline here to allow the answer to start exactly where the spinner was.
	_, _ = fmt.Fprint(ui.stderr, "\r"+ui.c(termClearLine))
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

	_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Engine (Step %d/%d)] Calling: %s%s\n",
		ui.c(colorCyan), ts, turn+1, maxTurns, strings.Join(names, ", "), ui.c(colorReset))

	if showTools {
		for _, fc := range calls {
			// Extract and display the reason if present
			if reason, ok := fc.Args["reason"].(string); ok && reason != "" {
				_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Reason] %s%s\n",
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
			_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Action] %s(%s)%s\n",
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
		_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: %s%s\n", ui.c(colorCyan), timestamp, name, snippet, ui.c(colorReset))
	}

	for _, b := range result.BinaryData {
		_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
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

	_, _ = fmt.Fprintf(stderr, "%s[%s] [%s] %s%s\n",
		ui.c(color), ui.getTimestamp(), prefix, msg, ui.c(colorReset))
}

func (r *stdUIRenderer) updateIndicatorFrame(ui uiState, frames []string, idx *int, startTime time.Time) {
	r.drawLoadingIndicator(ui, frames[*idx], startTime)
	*idx = (*idx + 1) % len(frames)
}
