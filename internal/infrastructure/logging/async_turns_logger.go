// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type asyncTurnsLogger struct {
	file   *os.File
	ch     chan string
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// NewAsyncTurnsLogger creates a new ports.TurnsLogger that writes to a file asynchronously.
func NewAsyncTurnsLogger(filePath string) (ports.TurnsLogger, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open turns log file: %w", err)
	}

	l := &asyncTurnsLogger{
		file: f,
		ch:   make(chan string, 100),
	}

	l.wg.Add(1)
	go l.processLogs()

	return l, nil
}

func (l *asyncTurnsLogger) processLogs() {
	defer l.wg.Done()
	for msg := range l.ch {
		_, _ = l.file.WriteString(msg)
	}
}

func (l *asyncTurnsLogger) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	var sb strings.Builder
	timestamp := status.Timestamp.Format("15:04:05")

	if !status.IsPostCall && !status.IsFinal {
		// Header
		sb.WriteString("────────────────────────────────────────────────────────────────────────────────\n")
		modeStr := ""
		if status.Mode != "" {
			modeStr = fmt.Sprintf(" - %s", status.Mode)
		}
		if status.MaxHistoryTurns > 0 {
			sb.WriteString(fmt.Sprintf("╭─⠿ Turn %d/%d%s\n", status.SessionTurns+1, status.MaxHistoryTurns, modeStr))
		} else {
			sb.WriteString(fmt.Sprintf("╭─⠿ Turn %d%s\n", status.SessionTurns+1, modeStr))
		}

		// Token line
		prefix := "~"
		sb.WriteString(fmt.Sprintf("[%s] Payload: %s%d/%d tokens%s\n", timestamp, prefix, status.Tokens, status.MaxHistoryTokens, modeStr))
	}

	if status.IsPostCall && status.Metrics != nil {
		m := status.Metrics
		// Token line (actual)
		modeStr := ""
		if status.Mode != "" {
			modeStr = fmt.Sprintf(" - %s", status.Mode)
		}
		sb.WriteString(fmt.Sprintf("[%s] Payload: %d/%d tokens%s\n", timestamp, m.PromptTokens, status.MaxHistoryTokens, modeStr))

		// Metrics line
		miss := m.PromptTokens - m.CachedTokens
		modelStr := ""
		displayName := m.Provider
		if displayName == "" {
			displayName = m.Model
		}
		if m.TrafficType == "ON_DEMAND_PRIORITY" {
			displayName = fmt.Sprintf("%s-priority", displayName)
		}
		if displayName != "" {
			modelStr = fmt.Sprintf(" [%s]", displayName)
		}

		totalTurnLatency := m.Duration + m.ToolDuration
		timingRaw := fmt.Sprintf("%.2fs (ΣT: %.2fs)", totalTurnLatency, m.CumulativeToolDuration)
		if !status.StartTime.IsZero() {
			totalSessionDuration := time.Since(status.StartTime).Seconds() // Using time.Since here as a fallback
			if status.CurrentTurns+1 > 0 {
				timingRaw = fmt.Sprintf("%s / %.2fs (%.2f)", timingRaw, totalSessionDuration, totalSessionDuration/float64(status.CurrentTurns+1))
			} else {
				timingRaw = fmt.Sprintf("%s / %.2fs", timingRaw, totalSessionDuration)
			}
		}

		costRaw := ""
		if m.Cost > 0 {
			costRaw = fmt.Sprintf(" ($%.4f)", m.Cost)
		}

		sb.WriteString(fmt.Sprintf("[%s]%s M: %d H: %d C: %d Th: %d %s [%s]\n", timestamp, modelStr, miss, m.CachedTokens, m.ResponseTokens, m.ThinkingTokens, costRaw, timingRaw))
	}

	if status.IsFinal {
		hitRate := 0.0
		if total := status.TotalM + status.TotalH; total > 0 {
			hitRate = float64(status.TotalH) / float64(total) * 100
		}

		turnCost := 0.0
		if status.Metrics != nil {
			turnCost = status.Metrics.Cost
		}

		costRaw := fmt.Sprintf(" ($%.4f $%.4f $%.4f $%.4f M: %d H: %d %.1f%% O: %d)",
			turnCost, status.TaskCost,
			status.SessionCost,
			status.DailyCost,
			status.TotalM,
			status.TotalH,
			hitRate,
			status.TotalO)

		sb.WriteString(fmt.Sprintf("╰─⠿ Ready%s\n", costRaw))
	}

	msg := sb.String()
	if msg != "" {
		select {
		case l.ch <- msg:
		default:
			// Buffer full, drop message to prevent blocking
		}
	}
}

func (l *asyncTurnsLogger) LogSystemMessage(ctx context.Context, msg string, level string) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	prefix := "System"
	switch level {
	case "error":
		prefix = "Error"
	case "warn":
		prefix = "Warning"
	case "info":
		prefix = "Info"
	}

	timestamp := time.Now().Format("15:04:05")
	logMsg := fmt.Sprintf("[%s] [%s] %s\n", timestamp, prefix, msg)

	select {
	case l.ch <- logMsg:
	default:
		// Buffer full, drop
	}
}

func (l *asyncTurnsLogger) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	close(l.ch)
	l.wg.Wait()
	return l.file.Close()
}
