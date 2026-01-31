// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// StdUIRenderer implements UIRenderer using standard output/error and Glamour.
type StdUIRenderer struct {
	sm *tools.SecurityManager
}

// NewStdUIRenderer creates a new StdUIRenderer.
func NewStdUIRenderer(sm *tools.SecurityManager) *StdUIRenderer {
	return &StdUIRenderer{sm: sm}
}

func (r *StdUIRenderer) LogUsage(m *types.Metrics, logFile string, startTime time.Time) {
	if logFile == "" || m == nil {
		return
	}

	miss := m.PromptTokens - m.CachedTokens
	newTokens := miss + m.ResponseTokens + m.ThinkingTokens
	percent := 0
	if m.TotalTokens > 0 {
		percent = int((int64(newTokens) * 100) / int64(m.TotalTokens))
	}

	timestamp := time.Now().Format("15:04:05")
	durationStr := fmt.Sprintf("%.2fs", m.Duration)
	if m.ToolDuration > 3.0 {
		durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
	}

	// [Time] H: 0 M: 45201 C: 217 T: 46102 N: 45418(98%) S: 1 Th: 1540 [13.5s / 15.2s]
	logLine := fmt.Sprintf("[%s] H: %d M: %d C: %d T: %d N: %d(%d%%) S: %d Th: %d [%s / %.2fs]\n",
		timestamp, m.CachedTokens, miss, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, durationStr, time.Since(startTime).Seconds())

	// Append to log file
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(logLine)
}

func (r *StdUIRenderer) LogTurnStatus(status TurnStatus) {
	gray := "\033[0;90m"
	reset := "\033[0m"
	timestamp := status.Timestamp.Format("15:04:05")

	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	printSystemLine := func(tks int, isActual bool) {
		tokenColor := reset
		if float64(tks) > float64(status.MaxHistoryTokens)*0.9 {
			tokenColor = "\033[0;31m" // Red if > 90%
		}

		if isActual {
			fmt.Fprintf(os.Stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				gray, timestamp, tokenColor, tks, gray, status.MaxHistoryTokens, reset)
		} else {
			fmt.Fprintf(os.Stderr, "%s[%s] [Turn (%s%d%s/%d)] Payload: ~%s%d%s/%d tokens%s\n",
				gray, timestamp, reset, status.CurrentTurns, gray, status.MaxHistoryTurns, tokenColor, tks, gray, status.MaxHistoryTokens, reset)
		}
	}

	if !status.IsPostCall {
		// 1. Print Payload Status (Pre-call estimate)
		printSystemLine(status.Tokens, false)
	} else if status.Metrics != nil {
		m := status.Metrics
		// 2. Re-print Payload Status (Post-call actual)
		printSystemLine(int(m.PromptTokens), true)

		// 3. Print Usage Metrics (Post-call)
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

		totalDuration := time.Since(status.StartTime).Seconds()
		durationStr := fmt.Sprintf("%.2fs", m.Duration)
		if m.ToolDuration > 3.0 {
			durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
		}

		fmt.Fprintf(os.Stderr, "%s[%s] %sH: %d M: %d%s C: %d T: %d N: %d(%d%%) S: %d Th: %d %s[%s%s%s / %.2fs%s]%s\n",
			gray, timestamp, hColor, m.CachedTokens, miss, gray, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, gray, reset, durationStr, gray, totalDuration, gray, reset)
	}
}

func (r *StdUIRenderer) RenderResponse(respContent *types.Content, showThoughts, rawOutput bool) {
	for _, part := range respContent.Parts {
		if showThoughts && part.Thought && part.Text != "" {
			func() {
				r.sm.TerminalLock()
				defer r.sm.TerminalUnlock()
				fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", time.Now().Format("15:04:05"), part.Text)
			}()
		}
	}
	for _, part := range respContent.Parts {
		if part.Text != "" && !part.Thought {
			if rawOutput {
				fmt.Print(part.Text)
				if !strings.HasSuffix(part.Text, "\n") {
					fmt.Println()
				}
			} else {
				r.renderMarkdown(part.Text)
			}
		}
	}
}

func (r *StdUIRenderer) renderMarkdown(text string) {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)

	out, err := renderer.Render(text)
	if err != nil {
		fmt.Print(text)
	} else {
		fmt.Print(out)
	}
}
