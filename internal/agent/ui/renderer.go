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
	LogUsage(m *llm.Metrics, logFile string, startTime time.Time)
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
}

// streamState holds the transient state for a single response stream.
type streamState struct {
	aggregated    *llm.Content
	totalText     strings.Builder
	thoughtActive bool
	showThoughts  bool
	rawOutput     bool
}

// NewStdUIRenderer creates a new StdUIRenderer.
func NewStdUIRenderer(sm *security.SecurityManager) *StdUIRenderer {
	tr, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)
	return &StdUIRenderer{
		sm:       sm,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		now:      time.Now,
		renderer: tr,
	}
}

// SetWriters allows overriding the output writers (primarily for testing).
func (r *StdUIRenderer) SetWriters(stdout, stderr io.Writer) {
	r.stdout = stdout
	r.stderr = stderr
}

func (r *StdUIRenderer) LogUsage(m *llm.Metrics, logFile string, startTime time.Time) {
	if logFile == "" || m == nil {
		return
	}

	m.Timestamp = r.now().Format(time.RFC3339)
	m.IsSummary = false

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
}

func (r *StdUIRenderer) renderMetricsLine(m *llm.Metrics, startTime time.Time) {
	if m == nil {
		return
	}
	timestamp := r.now().Format("15:04:05")

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
		totalDuration := r.now().Sub(startTime).Seconds()
		timingStr = fmt.Sprintf("%s%s%s / %.2fs%s", colors.ColorReset, durationStr, colors.ColorGray, totalDuration, colors.ColorGray)
	}

	fmt.Fprintf(r.stderr, "%s[%s] M: %d %sH: %d%s C: %d Th: %d %s[%s]%s\n",
		colors.ColorGray, timestamp, miss, hColor, m.CachedTokens, colors.ColorGray, m.ResponseTokens, m.ThinkingTokens, colors.ColorGray, timingStr, colors.ColorReset)
}

func (r *StdUIRenderer) LogTurnStatus(status events.TurnStatus) {
	timestamp := status.Timestamp.Format("15:04:05")

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
			fmt.Fprintf(r.stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				colors.ColorGray, timestamp, tokenColor, tks, colors.ColorGray, status.MaxHistoryTokens, colors.ColorReset)
		} else {
			fmt.Fprintf(r.stderr, "%s[%s] Payload: ~%s%d%s/%d tokens%s\n",
				colors.ColorGray, timestamp, tokenColor, tks, colors.ColorGray, status.MaxHistoryTokens, colors.ColorReset)
		}
	}

	if !status.IsPostCall {
		fmt.Fprintf(r.stderr, "\n%s────────────────────────────────────────────────────────────────────────────────%s\n", colors.ColorGray, colors.ColorReset)
		fmt.Fprintf(r.stderr, "%s╭─⠿ %sSession: %d/%d turns%s\n", colors.ColorGray, colors.ColorReset, status.SessionTurns+1, status.MaxHistoryTurns, colors.ColorGray)
		printSystemLine(status.Tokens, false)
		fmt.Fprintln(r.stderr) // Ensure visual gap before response
	} else if status.Metrics != nil {
		m := status.Metrics
		fmt.Fprintln(r.stderr) // Add vertical separation
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
		fmt.Fprintf(r.stderr, "%s╰─⠿ %sReady%s\n", colors.ColorGray, colors.ColorReset, costStr)
	}
}

func (r *StdUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	for _, part := range respContent.Parts {
		if showThoughts && part.Thought && part.Text != "" {
			sanitized := sanitizeForTerminal(part.Text)
			fmt.Fprintf(r.stderr, "%s[%s] [Thinking]\n%s%s\n", colors.ColorGray, r.now().Format("15:04:05"), sanitized, colors.ColorReset)
		}
	}
	for _, part := range respContent.Parts {
		if part.Text != "" && !part.Thought {
			if rawOutput {
				fmt.Fprint(r.stdout, part.Text)
				if !strings.HasSuffix(part.Text, "\n") {
					fmt.Fprintln(r.stdout)
				}
			} else {
				sanitized := sanitizeForTerminal(part.Text)
				r.renderMarkdown(sanitized)
			}
		}

		if part.InlineData != nil {
			fmt.Fprintf(r.stderr, "%s[%s] [Media] %s (%d bytes)%s\n",
				colors.ColorGray, r.now().Format("15:04:05"), part.InlineData.MIMEType, len(part.InlineData.Data), colors.ColorReset)
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
		func() {
			r.sm.TerminalLock()
			defer r.sm.TerminalUnlock()
			fmt.Fprint(r.stdout, colors.TermSaveCursor)
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
		r.safePrintStderr(fmt.Sprintf("%s[%s] [Thinking]\n", colors.ColorGray, r.now().Format("15:04:05")))
		state.thoughtActive = true
	}
	if state.showThoughts {
		sanitized := sanitizeForTerminal(part.Text)
		r.safePrintStderr(fmt.Sprintf("%s%s%s", colors.ColorGray, sanitized, colors.ColorReset))
	}
}

func (r *StdUIRenderer) handleTextPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	output := part.Text
	if !state.rawOutput {
		output = sanitizeForTerminal(part.Text)
	}
	func() {
		r.sm.TerminalLock()
		defer r.sm.TerminalUnlock()
		fmt.Fprint(r.stdout, output)
	}()
	state.totalText.WriteString(part.Text)
}

func (r *StdUIRenderer) handleInlineDataPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	r.safePrintStderr(fmt.Sprintf("\n%s[%s] [Media] %s (%d bytes)%s\n",
		colors.ColorGray, r.now().Format("15:04:05"), part.InlineData.MIMEType, len(part.InlineData.Data), colors.ColorReset))
}

func (r *StdUIRenderer) closeThinking(state *streamState) {
	if state.thoughtActive {
		r.safePrintStderr(colors.ColorReset + "\n")
		state.thoughtActive = false
	}
}

func (r *StdUIRenderer) safePrintStderr(msg string) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()
	fmt.Fprint(r.stderr, msg)
}

func (r *StdUIRenderer) finalizeOutput(state *streamState) {
	if !state.rawOutput {
		fullText := state.totalText.String()
		if fullText != "" {
			sanitized := sanitizeForTerminal(fullText)
			r.clearAndRenderMarkdown(sanitized)
		}
		func() {
			r.sm.TerminalLock()
			defer r.sm.TerminalUnlock()
			fmt.Fprintln(r.stdout)
		}()
	}
}

func (r *StdUIRenderer) clearAndRenderMarkdown(fullText string) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()
	fmt.Fprint(r.stdout, colors.TermRestoreCursor+colors.TermClearForward)
	r.renderMarkdown(fullText)
}

func (r *StdUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	var names []string
	for _, fc := range calls {
		names = append(names, fc.Name)
	}

	fmt.Fprintf(r.stderr, "%s[%s] [Tool Engine (Step %d/%d)] Calling: %s%s\n",
		colors.ColorCyan, r.now().Format("15:04:05"), turn+1, maxTurns, strings.Join(names, ", "), colors.ColorReset)

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
			fmt.Fprintf(r.stderr, "%s[%s] [Tool Action] %s(%s)%s\n",
				colors.ColorCyan, r.now().Format("15:04:05"), fc.Name, strings.Join(argParts, ", "), colors.ColorReset)
		}
	}
}

func (r *StdUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	if !showTools {
		return
	}

	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	timestamp := r.now().Format("15:04:05")

	if result.Text != "" {
		snippet := result.Text
		if len(snippet) > 200 {
			snippet = snippet[:197] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(r.stderr, "%s[%s] [Tool Result] %s: %s%s\n", colors.ColorCyan, timestamp, name, snippet, colors.ColorReset)
	}

	for _, b := range result.BinaryData {
		fmt.Fprintf(r.stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
			colors.ColorCyan, timestamp, name, b.MIMEType, len(b.Data), colors.ColorReset)
	}

	if m, ok := result.Metadata["metrics"].(*llm.Metrics); ok {
		r.renderMetricsLine(m, time.Time{}) // Render the usage line after the result
	}
}

func (r *StdUIRenderer) LogSystemMessage(msg string, level string) {
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

	fmt.Fprintf(r.stderr, "%s[%s] [%s] %s%s\n",
		color, r.now().Format("15:04:05"), prefix, msg, colors.ColorReset)
}

func (r *StdUIRenderer) renderMarkdown(text string) {
	fmt.Fprintf(r.stdout, "%s────────────────────────────────────────────────────────────────────────────────%s\n", colors.ColorGray, colors.ColorReset)
	out, err := r.renderer.Render(text)
	if err != nil {
		fmt.Fprint(r.stdout, text)
	} else {
		fmt.Fprint(r.stdout, out)
	}
}
