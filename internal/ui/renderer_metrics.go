// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func (r *stdUIRenderer) renderMetricsLine(ui uiState, m *llm.Metrics, startTime time.Time, turns int) {
	r.ioMu.Lock()
	defer r.ioMu.Unlock()
	r.renderMetricsLineLocked(ui, m, startTime, turns)
}

// formatModelDisplay builds the bracketed model display string, appending
// "-priority" for ON_DEMAND_PRIORITY traffic.
func formatModelDisplay(m *llm.Metrics) string {
	displayName := m.Provider
	if displayName == "" {
		displayName = m.Model
	}
	if strings.EqualFold(m.TrafficType, "ON_DEMAND_PRIORITY") {
		displayName = fmt.Sprintf("%s-priority", displayName)
	}
	if displayName != "" {
		return fmt.Sprintf(" [%s]", displayName)
	}
	return ""
}

// formatTimingStr builds the timing segment including total turn latency,
// cumulative tool duration, and optional session duration.
func formatTimingStr(ui uiState, totalTurnLatency, cumulativeToolDuration float64, startTime time.Time, turns int) string {
	timingStr := fmt.Sprintf("%s%.2fs %s(ΣT: %.2fs)%s",
		ui.c(colorReset), totalTurnLatency,
		ui.c(colorGray), cumulativeToolDuration,
		ui.c(colorGray))

	if !startTime.IsZero() {
		totalSessionDuration := ui.clock.Now().Sub(startTime).Seconds()
		if turns > 0 {
			timingStr = fmt.Sprintf("%s / %.2fs (%.2f)%s", timingStr, totalSessionDuration, totalSessionDuration/float64(turns), ui.c(colorGray))
		} else {
			timingStr = fmt.Sprintf("%s / %.2fs%s", timingStr, totalSessionDuration, ui.c(colorGray))
		}
	}
	return timingStr
}

func (r *stdUIRenderer) renderMetricsLineLocked(ui uiState, m *llm.Metrics, startTime time.Time, turns int) {
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

	modelStr := formatModelDisplay(m)

	totalTurnLatency := m.Duration + m.ToolDuration
	timingStr := formatTimingStr(ui, totalTurnLatency, m.CumulativeToolDuration, startTime, turns)

	// Prepare cost string
	costStr := ""
	if m.Cost > 0 {
		costStr = fmt.Sprintf(" %s($%.4f)%s", ui.c(colorGray), m.Cost, ui.c(colorGray))
	}

	// Prepare thinking-tokens segment. Suppressed when zero because
	// providers that do not expose a separate reasoning-token counter
	// (notably Anthropic — see issue #72) would otherwise always
	// display "Th: 0", misleading users into thinking no reasoning
	// occurred. Providers that DO expose reasoning tokens (Gemini,
	// OpenAI/DeepSeek) populate this field from their wire schema and
	// the "Th: <n>" segment continues to render whenever n > 0.
	thStr := ""
	if m.ThinkingTokens > 0 {
		thStr = fmt.Sprintf(" Th: %d", m.ThinkingTokens)
	}

	_, _ = fmt.Fprintf(stderr, "%s[%s]%s M: %d %sH: %d%s C: %d%s%s %s[%s]%s\n",
		ui.c(colorGray), timestamp, modelStr, miss, ui.c(hColor), m.CachedTokens, ui.c(colorGray), m.ResponseTokens, thStr, costStr, ui.c(colorGray), timingStr, ui.c(colorReset))
}

// metrics_shouldSample returns true if at least one second has elapsed since
// the last sample, or if no sample has ever been taken.
// Caller must hold at least r.mu.RLock.
func (r *stdUIRenderer) metrics_shouldSample(now time.Time) bool {
	return now.Sub(r.lastSampleTime) >= time.Second || r.lastSampleTime.IsZero()
}

// metrics_computeCPU calculates CPU usage percentage from raw counter deltas.
// When idleTicks > 0, host-level tick-based computation is used (1 - idle/total).
// Otherwise agent-level CPU-seconds are divided by wall-clock time per core.
// Returns math.NaN() as a sentinel when the computation cannot produce a valid
// update (e.g., zero delta-time or counter rollover). Callers must check with
// math.IsNaN before using the result.
func metrics_computeCPU(now time.Time, lastSampleTime time.Time, currentTotal, lastCPUTime, currentIdle, lastIdleTime int64) float64 {
	if lastSampleTime.IsZero() {
		return math.NaN()
	}
	if currentIdle > 0 {
		dTotal := float64(currentTotal - lastCPUTime)
		dIdle := float64(currentIdle - lastIdleTime)
		if dTotal > 0 {
			return (1.0 - (dIdle / dTotal)) * 100.0
		}
		return math.NaN()
	}
	// Agent-level from runtime/metrics
	dt := now.Sub(lastSampleTime).Seconds()
	if dt > 0 {
		dCPU := float64(currentTotal-lastCPUTime) / 1e9
		return (dCPU / dt) * 100.0 / float64(runtime.NumCPU())
	}
	return math.NaN()
}

func (r *stdUIRenderer) updateSystemMetrics(now time.Time) (float64, float64) {
	// 1. Check if update is needed under RLock
	r.mu.RLock()
	needsUpdate := r.metrics_shouldSample(now)
	cpuPercent := r.lastCPUPercent
	hostMemPercent := r.lastMemPercent
	r.mu.RUnlock()

	if needsUpdate {
		// 2. Perform I/O WITHOUT any lock
		currentTotal, currentIdle := r.metricsProvider.GetCPUStats()
		currentMem := r.metricsProvider.GetMemoryPercent()

		// 3. Update state under Write Lock
		r.mu.Lock()
		defer r.mu.Unlock()

		// Re-check update condition under write lock
		if r.metrics_shouldSample(now) {
			if cpu := metrics_computeCPU(now, r.lastSampleTime, currentTotal, r.lastCPUTime, currentIdle, r.lastIdleTime); !math.IsNaN(cpu) {
				cpuPercent = cpu
			}
			hostMemPercent = currentMem
			r.lastCPUTime = currentTotal
			r.lastIdleTime = currentIdle
			r.lastSampleTime = now
			r.lastCPUPercent = cpuPercent
			r.lastMemPercent = hostMemPercent
		} else {
			// Another goroutine updated it while we were doing I/O
			cpuPercent = r.lastCPUPercent
			hostMemPercent = r.lastMemPercent
		}
	}
	return cpuPercent, hostMemPercent
}

func (r *stdUIRenderer) renderTextLocked(ui uiState, part *llm.Part, raw bool) {
	if part.Text != "" && !part.IsThought {
		stdout := ui.stdout
		if raw {
			_, _ = fmt.Fprint(stdout, part.Text)
			if !strings.HasSuffix(part.Text, "\n") {
				_, _ = fmt.Fprintln(stdout)
			}
		} else {
			sanitized := sanitizeForTerminal(part.Text)
			r.renderMarkdownWithUILocked(ui, sanitized)
		}
	}
}

func (r *stdUIRenderer) renderThoughtLocked(ui uiState, part *llm.Part, showThoughts bool) {
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

func (r *stdUIRenderer) renderInlineDataLocked(ui uiState, part *llm.Part) {
	if part.InlineData != nil {
		ts := ui.getTimestamp()
		stderr := ui.stderr
		_, _ = fmt.Fprintf(stderr, "%s[%s] [Media] %s (%d bytes)%s\n",
			ui.c(colorGray), ts, part.InlineData.MIMEType, len(part.InlineData.Data), ui.c(colorReset))
	}
}

func (r *stdUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}

	ui := r.getUIState()

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	if !status.IsPostCall && !status.IsFinal {
		r.renderTurnHeader(ui, status)
	}

	if status.IsPostCall && status.Metrics != nil {
		r.renderPostCallStatus(ui, status)
	}

	if status.IsFinal {
		r.renderFinalSummary(ui, status)
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
		r.renderMetricsLine(ui, m, startTime, 0)
	}
}

// metrics_truncateArg truncates valStr to at most 189 characters, appending
// "..." when truncation occurs. This mirrors the truncation used for tool
// call argument values in the [Tool Action] line.
func metrics_truncateArg(valStr string) string {
	if len(valStr) > 189 {
		return valStr[:186] + "..."
	}
	return valStr
}

// metrics_formatToolCallArgLine builds a comma-separated "key: value" string
// from the function call arguments, excluding the "reason" key which is
// rendered separately as [Tool Reason]. Values longer than 189 characters
// are truncated.
func metrics_formatToolCallArgLine(args map[string]interface{}) string {
	var parts []string
	for k, v := range args {
		if k == "reason" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %v", k, metrics_truncateArg(fmt.Sprintf("%v", v))))
	}
	return strings.Join(parts, ", ")
}

// metrics_logToolReason prints a [Tool Reason] line to stderr if the function
// call contains a non-empty "reason" argument. Caller must hold r.ioMu.
func (r *stdUIRenderer) metrics_logToolReason(ui uiState, fc *llm.FunctionCall, ts string) {
	reason, ok := fc.Args["reason"].(string)
	if !ok || reason == "" {
		return
	}
	_, _ = fmt.Fprintf(ui.stderr, "%s[%s] [Tool Reason] %s%s\n",
		ui.c(colorGray), ts, reason, ui.c(colorReset))
}

func (r *stdUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	if r.locker != nil {
		r.locker.TerminalLock()
		defer r.locker.TerminalUnlock()
	}
	ui := r.getUIState()
	stderr := ui.stderr

	ts := ui.getTimestamp()

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	_, _ = fmt.Fprintf(stderr, "\r%s%s[%s] [Tool Engine] Step %d/%d%s\n", ui.c(termClearLine),
		ui.c(colorCyan), ts, turn+1, maxTurns, ui.c(colorReset))

	if showTools {
		for _, fc := range calls {
			r.metrics_logToolReason(ui, fc, ts)

			_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Action] %s(%s)%s\n",
				ui.c(colorMagenta), ts, fc.Name, metrics_formatToolCallArgLine(fc.Args), ui.c(colorReset))
		}
	}
}

func (r *stdUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
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

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	if result.Text != "" {
		snippet := result.Text
		if len(snippet) > 200 {
			snippet = snippet[:197] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		_, _ = fmt.Fprintf(stderr, "\r%s%s[%s] [Tool Result] %s: %s%s\n", ui.c(termClearLine), ui.c(colorCyan), timestamp, name, snippet, ui.c(colorReset))
	}

	for _, b := range result.BinaryData {
		_, _ = fmt.Fprintf(stderr, "\r%s%s[%s] [Tool Result] %s: Received %s (%d bytes)%s\n", ui.c(termClearLine),
			ui.c(colorCyan), timestamp, name, b.MIMEType, len(b.Data), ui.c(colorReset))
	}

	if m, ok := result.Metadata["metrics"].(*llm.Metrics); ok {
		r.renderMetricsLineLocked(ui, m, time.Time{}, 0) // Render the usage line after the result
	}
}

func (r *stdUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
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

	r.ioMu.Lock()
	defer r.ioMu.Unlock()

	_, _ = fmt.Fprintf(stderr, "\r%s%s[%s] [%s] %s%s\n", ui.c(termClearLine),
		ui.c(color), ui.getTimestamp(), prefix, msg, ui.c(colorReset))
}

func (r *stdUIRenderer) renderTurnHeader(ui uiState, status events.TurnStatus) {
	stderr := ui.stderr
	timestamp := status.Timestamp.Format("15:04:05")
	modeStr := ""
	if status.Mode != "" {
		modeStr = fmt.Sprintf(" - %s", status.Mode)
	}

	_, _ = fmt.Fprintf(stderr, "\n%s────────────────────────────────────────────────────────────────────────────────%s\n", ui.c(colorGray), ui.c(colorReset))

	if status.MaxHistoryTurns > 0 {
		_, _ = fmt.Fprintf(stderr, "%s╭─⠿ %sTurn %d/%d%s%s\n", ui.c(colorGray), ui.c(colorReset), status.SessionTurns+1, status.MaxHistoryTurns, modeStr, ui.c(colorGray))
	} else {
		_, _ = fmt.Fprintf(stderr, "%s╭─⠿ %sTurn %d%s%s\n", ui.c(colorGray), ui.c(colorReset), status.SessionTurns+1, modeStr, ui.c(colorGray))
	}

	r.printTokenLine(ui, timestamp, status.Tokens, status.MaxHistoryTokens, false, status.Mode)
	_, _ = fmt.Fprintln(stderr) // Ensure visual gap before response
}

func (r *stdUIRenderer) renderPostCallStatus(ui uiState, status events.TurnStatus) {
	stderr := ui.stderr
	timestamp := status.Timestamp.Format("15:04:05")
	m := status.Metrics

	if len(status.ToolReasons) > 0 {
		for _, reason := range status.ToolReasons {
			_, _ = fmt.Fprintf(stderr, "%s[%s] [Tool Reason] %s%s\n",
				ui.c(colorGray), timestamp, reason, ui.c(colorReset))
		}
	}

	r.printTokenLine(ui, timestamp, int(m.PromptTokens), status.MaxHistoryTokens, true, status.Mode)
	r.renderMetricsLineLocked(ui, m, status.StartTime, status.CurrentTurns+1)
}
