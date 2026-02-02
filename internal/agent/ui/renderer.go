// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
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
	"golang.org/x/term"
)

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

	miss := m.PromptTokens - m.CachedTokens
	newTokens := miss + m.ResponseTokens + m.ThinkingTokens
	percent := 0
	if m.TotalTokens > 0 {
		percent = int((int64(newTokens) * 100) / int64(m.TotalTokens))
	}

	timestamp := r.now().Format("15:04:05")
	durationStr := fmt.Sprintf("%.2fs", m.Duration)
	if m.ToolDuration > 3.0 {
		durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
	}

	logLine := fmt.Sprintf("[%s] H: %d M: %d C: %d T: %d N: %d(%d%%) S: %d Th: %d [%s / %.2fs]\n",
		timestamp, m.CachedTokens, miss, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, durationStr, r.now().Sub(startTime).Seconds())

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(logLine)
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
		fmt.Fprintf(r.stderr, "%s╭─⠿ %sTurn %d/%d%s\n", gray, reset, status.CurrentTurns+1, status.MaxHistoryTurns, gray)
		printSystemLine(status.Tokens, false)
	} else if status.Metrics != nil {
		m := status.Metrics
		printSystemLine(int(m.PromptTokens), true)

		miss := m.PromptTokens - m.CachedTokens
		newTokens := miss + m.ResponseTokens + m.ThinkingTokens
		percent := 0
		if m.TotalTokens > 0 {
			percent = int((int64(newTokens) * 100) / int64(m.TotalTokens))
		}

		hColor := gray
		if miss > m.CachedTokens {
			hColor = reset
		}

		// Cliff logic
		cliff := status.TieredThreshold
		if cliff <= 0 {
			cliff = config.DefaultTieredThreshold
		}
		warning := int(float64(cliff) * config.WarningRatio)

		statusColor := gray
		if int(m.PromptTokens) >= cliff {
			statusColor = "\033[0;31m" // Red
		} else if int(m.PromptTokens) >= warning {
			statusColor = "\033[0;33m" // Yellow
		}

		tColor := statusColor

		pColor := gray
		if percent < 20 {
			pColor = "\033[0;32m" // Green
		} else if percent > 70 {
			pColor = reset // White
		}

		// N color follows efficiency unless we hit the cliff penalty
		nColor := pColor
		if statusColor != gray {
			nColor = statusColor
		}

		totalDuration := r.now().Sub(status.StartTime).Seconds()
		durationStr := fmt.Sprintf("%.2fs", m.Duration)
		if m.ToolDuration > 3.0 {
			durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
		}

		fmt.Fprintf(r.stderr, "%s[%s] %sH: %d M: %d%s C: %d %sT: %s%d%s %sN: %s%d(%d%%)%s S: %d Th: %d %s[%s%s%s / %.2fs%s]%s\n",
			gray, timestamp, hColor, m.CachedTokens, miss, gray, m.ResponseTokens, gray, tColor, m.TotalTokens, gray, gray, nColor, newTokens, percent, gray, m.SearchQueries, m.ThinkingTokens, gray, reset, durationStr, gray, totalDuration, gray, reset)
		fmt.Fprintf(r.stderr, "%s╰─⠿ %sReady%s\n", gray, reset, gray)
	}
}

func (r *StdUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	for _, part := range respContent.Parts {
		if showThoughts && part.Thought && part.Text != "" {
			r.sm.TerminalLock()
			fmt.Fprintf(r.stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", r.now().Format("15:04:05"), part.Text)
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
				r.renderMarkdown(part.Text)
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
		r.safePrintStderr(fmt.Sprintf("\033[0;90m%s\033[0m", part.Text))
	}
}

func (r *StdUIRenderer) handleTextPart(state *streamState, part *llm.Part) {
	r.closeThinking(state)
	fmt.Fprint(r.stdout, part.Text)
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
			r.clearAndRenderMarkdown(fullText)
		}
		fmt.Fprintln(r.stdout)
	}
}

func (r *StdUIRenderer) clearAndRenderMarkdown(fullText string) {
	width := 80
	if f, ok := r.stdout.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil {
			width = w
		}
	}

	lines := r.calculateVisualLines(fullText, width)

	if lines > 0 {
		fmt.Fprintf(r.stdout, "\r\033[%dA\033[J", lines)
	}
	r.renderMarkdown(fullText)
}

func (r *StdUIRenderer) calculateVisualLines(text string, width int) int {
	if width <= 0 {
		width = 80
	}
	lines := 0
	currentLineLen := 0
	for _, runeValue := range text {
		if runeValue == '\n' {
			lines++
			currentLineLen = 0
			continue
		}
		currentLineLen++
		if currentLineLen >= width {
			lines++
			currentLineLen = 0
		}
	}
	if currentLineLen > 0 {
		lines++
	}
	return lines
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

	fmt.Fprintf(r.stderr, "%s[%s] %s[Tool Engine (%s%d%s/%d)] Calling: %s%s\n",
		cyan, r.now().Format("15:04:05"), cyan, reset, turn+1, cyan, maxTurns, strings.Join(names, ", "), reset)

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
