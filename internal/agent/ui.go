// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
	"golang.org/x/term"
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
			r.sm.TerminalLock()
			fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n%s\033[0m\n", time.Now().Format("15:04:05"), part.Text)
			r.sm.TerminalUnlock()
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

		if part.InlineData != nil {
			r.sm.TerminalLock()
			fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Media] %s (%d bytes)\033[0m\n",
				time.Now().Format("15:04:05"), part.InlineData.MIMEType, len(part.InlineData.Data))
			r.sm.TerminalUnlock()
		}
	}
}

// StreamResponse provides a channel to stream content parts and a finalizer to get the aggregated content.
func (r *StdUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *types.Content, func() *types.Content) {
	ch := make(chan *types.Content, 100)
	aggregated := &types.Content{Role: "model"}
	var wg sync.WaitGroup
	wg.Add(1)

	// Track state for incremental rendering and cleanup
	thoughtActive := false
	var totalText strings.Builder

	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case content, ok := <-ch:
				if !ok {
					if thoughtActive {
						r.sm.TerminalLock()
						fmt.Fprintf(os.Stderr, "\033[0m\n")
						r.sm.TerminalUnlock()
					}
					return
				}

				for _, part := range content.Parts {
					// Aggregate
					r.mergePart(aggregated, part)

					// Incremental Render
					if part.Thought {
						if !thoughtActive && showThoughts {
							r.sm.TerminalLock()
							fmt.Fprintf(os.Stderr, "\033[0;90m[%s] [Thinking]\n", time.Now().Format("15:04:05"))
							r.sm.TerminalUnlock()
							thoughtActive = true
						}
						if showThoughts {
							r.sm.TerminalLock()
							fmt.Fprintf(os.Stderr, "\033[0;90m%s\033[0m", part.Text)
							r.sm.TerminalUnlock()
						}
					} else if part.Text != "" {
						if thoughtActive {
							r.sm.TerminalLock()
							fmt.Fprintf(os.Stderr, "\033[0m\n") // Close thinking block
							r.sm.TerminalUnlock()
							thoughtActive = false
						}
						// For text, we stream it raw to terminal
						fmt.Print(part.Text)
						totalText.WriteString(part.Text)
					}

					if part.InlineData != nil {
						if thoughtActive {
							r.sm.TerminalLock()
							fmt.Fprintf(os.Stderr, "\033[0m\n")
							r.sm.TerminalUnlock()
							thoughtActive = false
						}
						r.sm.TerminalLock()
						fmt.Fprintf(os.Stderr, "\n\033[0;90m[%s] [Media] %s (%d bytes)\033[0m\n",
							time.Now().Format("15:04:05"), part.InlineData.MIMEType, len(part.InlineData.Data))
						r.sm.TerminalUnlock()
					}
				}
			}
		}
	}()

	var once sync.Once
	return ch, func() *types.Content {
		once.Do(func() {
			close(ch)
			wg.Wait()
			if !rawOutput {
				fullText := totalText.String()
				if fullText != "" {
					// 1. Calculate how many lines the raw text occupied to "clear" it
					width, _, err := term.GetSize(int(os.Stdout.Fd()))
					if err != nil {
						width = 80 // Fallback
					}

					lines := 0
					currentLineLen := 0
					for _, r := range fullText {
						if r == '\n' {
							lines++
							currentLineLen = 0
						} else {
							currentLineLen++
							if currentLineLen >= width {
								lines++
								currentLineLen = 0
							}
						}
					}
					// If there was any text remaining on the last line, it counts as a line
					if currentLineLen > 0 {
						lines++
					}

					// 2. Move cursor up and clear from there
					if lines > 0 {
						// \033[A moves cursor up, \r moves to start, \033[J clears to end of screen
						fmt.Printf("\r\033[%dA\033[J", lines)
					}

					// 3. Render pretty version
					r.renderMarkdown(fullText)
				}
				fmt.Println() // Ensure we end on a new line
			}
		})
		return aggregated
	}
}

func (r *StdUIRenderer) mergePart(dst *types.Content, src *types.Part) {
	// If it's a function call/response, just append
	if src.FunctionCall != nil || src.FunctionResponse != nil || src.InlineData != nil {
		dst.Parts = append(dst.Parts, src)
		return
	}

	// For text/thought, try to append to last part if same type
	if len(dst.Parts) > 0 {
		last := dst.Parts[len(dst.Parts)-1]
		if last.Thought == src.Thought && last.FunctionCall == nil && last.FunctionResponse == nil && last.InlineData == nil {
			last.Text += src.Text
			return
		}
	}

	// Otherwise append new part
	dst.Parts = append(dst.Parts, &types.Part{
		Text:    src.Text,
		Thought: src.Thought,
	})
}

func (r *StdUIRenderer) LogToolResult(name string, result types.ToolResult, showTools bool) {
	if !showTools {
		return
	}

	r.sm.TerminalLock()
	defer r.sm.TerminalUnlock()

	cyan := "\033[0;36m"
	reset := "\033[0m"
	timestamp := time.Now().Format("15:04:05")

	if result.Text != "" {
		snippet := result.Text
		if len(snippet) > 200 {
			snippet = snippet[:197] + "..."
		}
		// Clean up newlines for a compact log
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		fmt.Fprintf(os.Stderr, "%s[%s] [Tool Result] %s: %s%s\n", cyan, timestamp, name, snippet, reset)
	}

	for _, b := range result.BinaryData {
		fmt.Fprintf(os.Stderr, "%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n",
			cyan, timestamp, name, b.MIMEType, len(b.Data), reset)
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
