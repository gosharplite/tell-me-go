// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

type asyncTurnsLogger struct {
	file   infra_persistence.File
	ch     chan string
	wg     sync.WaitGroup
	logger *slog.Logger
	clock  clock.Clock
	closed bool
	mu     sync.RWMutex
}

// NewAsyncTurnsLogger creates a new ports.TurnsLogger that writes to a file asynchronously.
func NewAsyncTurnsLogger(ctx context.Context, fs infra_persistence.FileSystem, filePath string, logger *slog.Logger) (ports.TurnsLogger, error) {
	if logger == nil {
		logger = slog.Default()
	}

	f, err := fs.OpenFile(ctx, filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open turns log file: %w", err)
	}

	tl := &asyncTurnsLogger{
		file:   f,
		ch:     make(chan string, 1000),
		logger: logger,
		clock:  clock.RealClock{}, // Initialize internal clock
	}

	// [REFACTOR] No longer starts fire-and-forget goroutine here.
	// The worker loop is now managed via Listen(ctx).

	return tl, nil
}

// Listen starts the worker loop and blocks until the context is canceled.
func (l *asyncTurnsLogger) Listen(ctx context.Context) error {
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return nil
	}
	l.wg.Add(1)
	l.mu.RUnlock()

	defer l.wg.Done()

	for {
		select {
		case <-ctx.Done():
			l.drainAndSync()
			return nil
		case msg, ok := <-l.ch:
			if !ok {
				return nil
			}
			l.processMessage(msg)
		}
	}
}

func (l *asyncTurnsLogger) processMessage(msg string) {
	if _, err := l.file.Write([]byte(msg)); err != nil {
		l.logger.Warn("failed to write to turns log", "error", err)
		return
	}
	// Smart batching: only fsync when the channel buffer is fully drained
	if len(l.ch) == 0 {
		if err := l.file.Sync(); err != nil {
			l.logger.Warn("failed to sync turns log", "error", err)
		}
	}
}

func (l *asyncTurnsLogger) drainAndSync() {
	for {
		select {
		case msg := <-l.ch:
			if _, err := l.file.Write([]byte(msg)); err != nil {
				l.logger.Warn("failed to write to turns log on shutdown", "error", err)
			}
		default:
			// Ensure everything is persisted before exiting
			if err := l.file.Sync(); err != nil {
				l.logger.Warn("failed to sync turns log on shutdown", "error", err)
			}
			return
		}
	}
}

func (l *asyncTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {
	now := l.clock.Now()
	switch ev := e.(type) {
	case events.SystemMessageEvent:
		l.log(l.formatSystemMessageForLog(ev.Message, ev.Level, now))
	case events.TurnStatusEvent:
		l.log(l.formatTurnStatusForLog(ev.Status, now))
	}
}

func (l *asyncTurnsLogger) log(msg string) {
	if msg == "" {
		return
	}
	// Ensure string ends with newline
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.closed {
		return
	}

	select {
	case l.ch <- msg:
	default:
		l.logger.Warn("turns logger buffer full, dropping message")
	}
}

func (l *asyncTurnsLogger) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	close(l.ch)
	l.mu.Unlock()

	l.wg.Wait()
	return l.file.Close()
}

func (l *asyncTurnsLogger) formatSystemMessageForLog(msg string, level string, timestamp time.Time) string {
	prefix := "System"
	switch level {
	case "error":
		prefix = "Error"
	case "warn":
		prefix = "Warning"
	case "info":
		prefix = "Info"
	}

	return fmt.Sprintf("[%s] [%s] %s", timestamp.Format("15:04:05"), prefix, msg)
}

func (l *asyncTurnsLogger) formatTurnStatusForLog(status events.TurnStatus, now time.Time) string {
	var sb strings.Builder
	timestamp := status.Timestamp.Format("15:04:05")

	if !status.IsPostCall && !status.IsFinal {
		l.renderTurnHeader(&sb, status, timestamp)
	}

	if status.IsPostCall && status.Metrics != nil {
		l.renderTurnMetrics(&sb, status, now, timestamp)
	}

	if status.IsFinal {
		l.renderTurnFooter(&sb, status)
	}

	return sb.String()
}

func (l *asyncTurnsLogger) renderTurnHeader(sb *strings.Builder, status events.TurnStatus, timestamp string) {
	// Header
	sb.WriteString("────────────────────────────────────────────────────────────────────────────────\n")
	modeStr := ""
	if status.Mode != "" {
		modeStr = fmt.Sprintf(" - %s", status.Mode)
	}
	if status.MaxHistoryTurns > 0 {
		fmt.Fprintf(sb, "╭─⠿ Turn %d/%d%s\n", status.SessionTurns+1, status.MaxHistoryTurns, modeStr)
	} else {
		fmt.Fprintf(sb, "╭─⠿ Turn %d%s\n", status.SessionTurns+1, modeStr)
	}

	// Token line
	prefix := "~"
	fmt.Fprintf(sb, "[%s] Payload: %s%d/%d tokens%s\n", timestamp, prefix, status.Tokens, status.MaxHistoryTokens, modeStr)
}

func (l *asyncTurnsLogger) renderTurnMetrics(sb *strings.Builder, status events.TurnStatus, now time.Time, timestamp string) {
	m := status.Metrics
	// Token line (actual)
	modeStr := ""
	if status.Mode != "" {
		modeStr = fmt.Sprintf(" - %s", status.Mode)
	}
	fmt.Fprintf(sb, "[%s] Payload: %d/%d tokens%s\n", timestamp, m.PromptTokens, status.MaxHistoryTokens, modeStr)

	// Metrics line
	miss := m.PromptTokens - m.CachedTokens
	modelStr := ""
	displayName := m.Provider
	if displayName == "" {
		displayName = m.Model
	}
	if strings.EqualFold(m.TrafficType, "ON_DEMAND_PRIORITY") {
		displayName = fmt.Sprintf("%s-priority", displayName)
	}
	if displayName != "" {
		modelStr = fmt.Sprintf(" [%s]", displayName)
	}

	totalTurnLatency := m.Duration + m.ToolDuration
	timingRaw := fmt.Sprintf("%.2fs (ΣT: %.2fs)", totalTurnLatency, m.CumulativeToolDuration)
	if !status.StartTime.IsZero() {
		totalSessionDuration := now.Sub(status.StartTime).Seconds()
		if status.CurrentTurns+1 > 0 {
			timingRaw = fmt.Sprintf("%s / %.2fs (%.2f)", timingRaw, totalSessionDuration, totalSessionDuration/float64(status.CurrentTurns+1))
		} else {
			timingRaw = fmt.Sprintf("%s / %.2fs", timingRaw, totalSessionDuration)
		}
	}

	// Prepare thinking-tokens segment. Suppressed when zero — kept in
	// lockstep with the on-screen renderer (see internal/ui/renderer.go
	// and issue #72) so that grep-based cost reconciliation between
	// terminal output and log files stays consistent. Providers that
	// do not separately report reasoning tokens (notably Anthropic)
	// will never emit this segment; Gemini and OpenAI/DeepSeek will
	// emit it whenever reasoning actually fired.
	thStr := ""
	if m.ThinkingTokens > 0 {
		thStr = fmt.Sprintf(" Th: %d", m.ThinkingTokens)
	}

	if m.Cost > 0 {
		fmt.Fprintf(sb, "[%s]%s M: %d H: %d C: %d%s ($%.4f) [%s]\n", timestamp, modelStr, miss, m.CachedTokens, m.ResponseTokens, thStr, m.Cost, timingRaw)
	} else {
		fmt.Fprintf(sb, "[%s]%s M: %d H: %d C: %d%s [%s]\n", timestamp, modelStr, miss, m.CachedTokens, m.ResponseTokens, thStr, timingRaw)
	}
}

func (l *asyncTurnsLogger) renderTurnFooter(sb *strings.Builder, status events.TurnStatus) {
	hitRate := 0.0
	if total := status.TotalM + status.TotalH; total > 0 {
		hitRate = float64(status.TotalH) / float64(total) * 100
	}

	turnCost := 0.0
	if status.Metrics != nil {
		turnCost = status.Metrics.Cost
	}

	fmt.Fprintf(sb, "╰─⠿ Ready ($%.4f $%.4f $%.4f $%.4f M: %d H: %d %.1f%% O: %d)\n",
		turnCost, status.TaskCost,
		status.SessionCost,
		status.DailyCost,
		status.TotalM,
		status.TotalH,
		hitRate,
		status.TotalO)
}
