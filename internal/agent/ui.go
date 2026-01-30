// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func (a *Agent) logUsage(m *types.Metrics) {
	if a.logFile == "" || m == nil {
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
		timestamp, m.CachedTokens, miss, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, durationStr, time.Since(a.startTime).Seconds())

	// Append to log file
	f, err := os.OpenFile(a.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(logLine)
}

func (a *Agent) logTurnStatus(currentTurns, tokens int, m *types.Metrics, isPostCall bool) {
	gray := "\033[0;90m"
	reset := "\033[0m"
	timestamp := time.Now().Format("15:04:05")

	a.sm.TerminalLock()
	defer a.sm.TerminalUnlock()

	printSystemLine := func(tks int, isActual bool) {
		tokenColor := reset
		if float64(tks) > float64(a.maxHistoryTokens)*0.9 {
			tokenColor = "\033[0;31m" // Red if > 90%
		}

		if isActual {
			fmt.Fprintf(os.Stderr, "%s[%s] Payload: %s%d%s/%d tokens%s\n",
				gray, timestamp, tokenColor, tks, gray, a.maxHistoryTokens, reset)
		} else {
			fmt.Fprintf(os.Stderr, "%s[%s] [Turn (%s%d%s/%d)] Payload: ~%s%d%s/%d tokens%s\n",
				gray, timestamp, reset, currentTurns, gray, a.maxHistoryTurns, tokenColor, tks, gray, a.maxHistoryTokens, reset)
		}
	}

	if !isPostCall {
		// 1. Print Payload Status (Pre-call estimate)
		printSystemLine(tokens, false)
	} else if m != nil {
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

		totalDuration := time.Since(a.startTime).Seconds()
		durationStr := fmt.Sprintf("%.2fs", m.Duration)
		if m.ToolDuration > 3.0 {
			durationStr = fmt.Sprintf("%.2fs+%.0fs", m.Duration, m.ToolDuration)
		}

		fmt.Fprintf(os.Stderr, "%s[%s] %sH: %d M: %d%s C: %d T: %d N: %d(%d%%) S: %d Th: %d %s[%s%s%s / %.2fs%s]%s\n",
			gray, timestamp, hColor, m.CachedTokens, miss, gray, m.ResponseTokens, m.TotalTokens, newTokens, percent, m.SearchQueries, m.ThinkingTokens, gray, reset, durationStr, gray, totalDuration, gray, reset)
	}
}

func (a *Agent) renderResponse(respContent *types.Content) {
	for _, part := range respContent.Parts {
		if a.showThoughts && part.Thought && part.Text != "" {
			a.sm.TerminalLock()
			fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", time.Now().Format("15:04:05"), part.Text)
			a.sm.TerminalUnlock()
		}
	}
	for _, part := range respContent.Parts {
		if part.Text != "" && !part.Thought {
			if a.rawOutput {
				fmt.Print(part.Text)
				if !strings.HasSuffix(part.Text, "\n") {
					fmt.Println()
				}
			} else {
				a.renderMarkdown(part.Text)
			}
		}
	}
}

func (a *Agent) renderMarkdown(text string) {
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithEmoji(),
	)

	out, err := r.Render(text)
	if err != nil {
		fmt.Print(text)
	} else {
		fmt.Print(out)
	}
}
