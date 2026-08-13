// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package metricsfmt

import (
	"fmt"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// FormatMetricsLine renders a single-line post-call metrics summary from
// a TurnStatus. Returns an empty string if Metrics is nil.
func FormatMetricsLine(ts events.TurnStatus) string {
	if ts.Metrics == nil {
		return ""
	}
	miss := ts.Metrics.PromptTokens - ts.Metrics.CachedTokens
	var parts []string

	parts = append(parts, fmt.Sprintf("[%s]", ts.Timestamp.Format("15:04:05")))

	display := ts.Metrics.Provider
	if display == "" {
		display = ts.Metrics.Model
	}
	if display != "" {
		parts = append(parts, fmt.Sprintf("[%s]", display))
	}

	parts = append(parts, fmt.Sprintf("M: %d H: %d C: %d",
		miss, ts.Metrics.CachedTokens, ts.Metrics.ResponseTokens))

	if ts.Metrics.ThinkingTokens > 0 {
		parts = append(parts, fmt.Sprintf("Th: %d", ts.Metrics.ThinkingTokens))
	}

	if ts.Metrics.Cost > 0 {
		parts = append(parts, fmt.Sprintf("($%.4f)", ts.Metrics.Cost))
	}

	totalLatency := ts.Metrics.Duration + ts.Metrics.ToolDuration
	timing := fmt.Sprintf("[%.2fs (ΣT: %.2fs)]",
		totalLatency, ts.Metrics.CumulativeToolDuration)
	if !ts.StartTime.IsZero() {
		sessionDur := time.Since(ts.StartTime).Seconds()
		turns := ts.CurrentTurns + 1
		if turns > 0 {
			timing = fmt.Sprintf("%s / %.2fs (%.2f)",
				timing, sessionDur, sessionDur/float64(turns))
		} else {
			timing = fmt.Sprintf("%s / %.2fs", timing, sessionDur)
		}
	}
	parts = append(parts, timing)

	return strings.Join(parts, " ")
}

// FormatFinalLine renders the "Ready" summary line when the session is
// complete. turnCost is the cost of the current turn (from Metrics.Cost).
func FormatFinalLine(ts events.TurnStatus, turnCost float64) string {
	hitRate := 0.0
	if total := ts.TotalM + ts.TotalH; total > 0 {
		hitRate = float64(ts.TotalH) / float64(total) * 100
	}
	return fmt.Sprintf("╰─⠿ Ready ($%.4f $%.4f $%.4f M: %d H: %d %.1f%% O: %d)",
		turnCost, ts.TaskCost, ts.SessionCost,
		ts.TotalM, ts.TotalH, hitRate, ts.TotalO)
}
