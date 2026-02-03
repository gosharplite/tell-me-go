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
	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
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

func (r *StdUIRenderer) LogTurnStatus(status events.TurnStatus) {
	gray := "\033[0;90m"
	reset := "\033[0m"
	timestamp := status.Timestamp.Format("15:04:05")

	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	printSystemLine := func(tks int, isActual bool) {
		tokenColor := reset
		if float64(tks) > float64(status.MaxHistoryTokens)*config.WarningRatio {
			tokenColor = "\033[0;33m" // Yellow caution
		}
		if float64(tks) > float64(status.MaxHistoryTokens) {
			tokenColor = "\033[0;31m" // Red limit
		}

		if isActual {
			fmt.Fprintf(r.stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				gray, timestamp, tokenColor, tks, gray, status.MaxHistoryTokens, reset)
		} else {
			fmt.Fprintf(r.stderr, "%s[%s] Payload: ~%s%d%s/%d tokens%s\n",
				gray, timestamp, tokenColor, tks, gray, status.MaxHistoryTokens, reset)
		}
	}

	if !status.IsPostCall {
		fmt.Fprintf(r.stderr, "\n\033[0;90m────────────────────────────────────────────────────────────────────────────────\033[0m\n")
		fmt.Fprintf(r.stderr, "%s╭─⠿ %sSession: %d/%d turns%s\n", gray, reset, status.SessionTurns+1, status.MaxHistoryTurns, gray)
		printSystemLine(status.Tokens, false)
	} else if status.Metrics != nil {
		m := status.Metrics
		fmt.Fprintln(r.stderr) // Add vertical separation
		printSystemLine(int(m.PromptTokens), true)

		miss := m.PromptTokens - m.CachedTokens

		hColor := gray
		if miss > m.CachedTokens {
			hColor = reset
		}

		totalDuration := r.now().Sub(status.StartTime).Seconds()
		durationStr := fmt.Sprintf("%.2fs", m.Duration)
		if m.ToolDuration > 3.0 {
			durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
		}

		fmt.Fprintf(r.stderr, "%s[%s] M: %d %sH: %d%s C: %d Th: %d %s[%s%s%s / %.2fs%s]%s\n",
			gray, timestamp, miss, hColor, m.CachedTokens, gray, m.ResponseTokens, m.ThinkingTokens, gray, reset, durationStr, gray, totalDuration, gray, reset)

		costStr := ""
		if status.SessionCost > 0 {
			hitRate := 0.0
			if total := status.TotalM + status.TotalH; total > 0 {
				hitRate = float64(status.TotalH) / float64(total) * 100
			}
			green := "\033[0;32m"
			// Format: (TurnCost TaskCost SessionCost M: ... H: ... O: ...)
			// Highlight ONLY the SessionCost ($1.4745 in user example).
			costStr = fmt.Sprintf(" %s($%.4f $%.4f %s$%.4f%s M: %d H: %d %.1f%% O: %d)%s",
				gray,
				status.Metrics.Cost, status.TaskCost,
				green, status.SessionCost, gray,
				status.TotalM,
				status.TotalH,
				hitRate,
				status.TotalO,
				gray)
		}
		fmt.Fprintf(r.stderr, "%s╰─⠿ %sReady%s\n", gray, reset, costStr)
	}
}

func (r *StdUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	for _, part := range respContent.Parts {
		if showThoughts && part.Thought && part.Text != "" {
			r.sm.TerminalLock()
			sanitized := sanitizeForTerminal(part.Text)
			fmt.Fprintf(r.stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", r.now().Format("15:04:05"), sanitized)
			r.sm.TerminalUnlock()
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
			r.sm.TerminalLock()
			fmt.Fprintf(r.stderr, "\033[0;90m[%s] [Media] %s (%d bytes)\033[0m\n",
				r.now().Format("15:04:05"), part.InlineData.MIMEType, len(part.InlineData.Data))
			r.sm.TerminalUnlock()
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
		r.sm.TerminalLock()
		fmt.Fprint(r.stdout, "\0337")
		r.sm.TerminalUnlock()
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
		r.safePrintStderr(fmt.Sprintf("\033[0;90m[%s] [Thinking]\n", r.now().Format("15:04:05")))
		state.thoughtActive = true
	}
	if state.showThoughts {
		sanitized := sanitizeForTerminal(part.Text)
		r.safePrintStderr(fmt.Sprintf("\033[0;90m%s\033[0m", sanitized))
	}
}

func (r *StdUIRenderer) handleTextPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	output := part.Text
	if !state.rawOutput {
		output = sanitizeForTerminal(part.Text)
	}
	fmt.Fprint(r.stdout, output)
	state.totalText.WriteString(part.Text)
}

func (r *StdUIRenderer) handleInlineDataPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	r.safePrintStderr(fmt.Sprintf("\n\033[0;90m[%s] [Media] %s (%d bytes)\033[0m\n",
		r.now().Format("15:04:05"), part.InlineData.MIMEType, len(part.InlineData.Data)))
}

func (r *StdUIRenderer) closeThinking(state *streamState) {
	if state.thoughtActive {
		r.safePrintStderr("\033[0m\n")
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
		fmt.Fprintln(r.stdout)
	}
}

func (r *StdUIRenderer) clearAndRenderMarkdown(fullText string) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()
	fmt.Fprint(r.stdout, "\0338\033[J")
	r.renderMarkdown(fullText)
}

func (r *StdUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	var names []string
	for _, fc := range calls {
		names = append(names, fc.Name)
	}

	cyan := "\033[0;36m"
	reset := "\033[0m"

	fmt.Fprintf(r.stderr, "%s[%s] %s[Tool Engine (Step %d/%d)] Calling: %s%s\n",
		cyan, r.now().Format("15:04:05"), cyan, turn+1, maxTurns, strings.Join(names, ", "), reset)

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
			fmt.Fprintf(r.stderr, "\033[0;36m[%s] [Tool Action] %s(%s)\033[0m\n",
				r.now().Format("15:04:05"), fc.Name, strings.Join(argParts, ", "))
		}
	}
}

func (r *StdUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	if !showTools {
		return
	}

	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	cyan := "\033[0;36m"
	reset := "\033[0m"
	timestamp := r.now().Format("15:04:05")

	if result.Text != "" {
		snippet := result.Text
		if len(snippet) > 200 {
			snippet = snippet[:197] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(r.stderr, "%s[%s] [Tool Result] %s: %s%s\n", cyan, timestamp, name, snippet, reset)
	}

	for _, b := range result.BinaryData {
		fmt.Fprintf(r.stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
			cyan, timestamp, name, b.MIMEType, len(b.Data), reset)
	}
}

func (r *StdUIRenderer) LogSystemMessage(msg string, level string) {
	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	color := "\033[0;90m"
	prefix := "System"

	switch level {
	case "error":
		color = "\033[0;31m"
		prefix = "Error"
	case "warn":
		color = "\033[0;90m"
		prefix = "Warning"
	case "info":
		color = "\033[0;36m"
		prefix = "Info"
	}

	fmt.Fprintf(r.stderr, "%s[%s] [%s] %s\033[0m\n",
		color, r.now().Format("15:04:05"), prefix, msg)
}

func (r *StdUIRenderer) renderMarkdown(text string) {
	fmt.Fprintf(r.stdout, "\033[0;90m────────────────────────────────────────────────────────────────────────────────\033[0m\n")
	out, err := r.renderer.Render(text)
	if err != nil {
		fmt.Fprint(r.stdout, text)
	} else {
		fmt.Fprint(r.stdout, out)
	}
}
