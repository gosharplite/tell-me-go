// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// fullMetricsStatus returns a TurnStatus exercising every optional part of
// FormatMetricsLine (provider, thinking tokens, cost, timing) with a zero
// StartTime so the output is fully deterministic.
func fullMetricsStatus() events.TurnStatus {
	return events.TurnStatus{
		Timestamp: time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC),
		Metrics: &llm.Metrics{
			PromptTokens:           1000,
			CachedTokens:           800,
			ResponseTokens:         50,
			ThinkingTokens:         120,
			Cost:                   0.0012,
			Duration:               5.0,
			ToolDuration:           2.0,
			CumulativeToolDuration: 1.5,
			Provider:               "deepseek-pro",
		},
	}
}

func TestFormatMetricsLine(t *testing.T) {
	t.Run("nil metrics returns empty string", func(t *testing.T) {
		got := FormatMetricsLine(events.TurnStatus{})
		if got != "" {
			t.Errorf("FormatMetricsLine() with nil Metrics = %q; want empty", got)
		}
	})

	t.Run("renders full metrics line", func(t *testing.T) {
		got := FormatMetricsLine(fullMetricsStatus())
		want := "[14:30:00] [deepseek-pro] M: 200 H: 800 C: 50 Th: 120 ($0.0012) [7.00s (ΣT: 1.50s)]"
		if got != want {
			t.Errorf("FormatMetricsLine() = %q; want %q", got, want)
		}
	})

	t.Run("omits thinking tokens when zero", func(t *testing.T) {
		ts := fullMetricsStatus()
		ts.Metrics.ThinkingTokens = 0
		got := FormatMetricsLine(ts)
		if strings.Contains(got, "Th:") {
			t.Errorf("FormatMetricsLine() = %q; want no Th: segment", got)
		}
	})

	t.Run("omits cost when zero", func(t *testing.T) {
		ts := fullMetricsStatus()
		ts.Metrics.Cost = 0
		got := FormatMetricsLine(ts)
		if strings.Contains(got, "($") {
			t.Errorf("FormatMetricsLine() = %q; want no cost segment", got)
		}
	})

	t.Run("provider falls back to model", func(t *testing.T) {
		ts := fullMetricsStatus()
		ts.Metrics.Provider = ""
		ts.Metrics.Model = "deepseek-v4-pro"
		got := FormatMetricsLine(ts)
		if !strings.Contains(got, "[deepseek-v4-pro]") {
			t.Errorf("FormatMetricsLine() = %q; want model fallback [deepseek-v4-pro]", got)
		}
	})

	t.Run("omits provider bracket when both empty", func(t *testing.T) {
		ts := fullMetricsStatus()
		ts.Metrics.Provider = ""
		ts.Metrics.Model = ""
		got := FormatMetricsLine(ts)
		want := "[14:30:00] M: 200 H: 800 C: 50 Th: 120 ($0.0012) [7.00s (ΣT: 1.50s)]"
		if got != want {
			t.Errorf("FormatMetricsLine() = %q; want %q", got, want)
		}
	})

	t.Run("zero StartTime omits session duration segment", func(t *testing.T) {
		got := FormatMetricsLine(fullMetricsStatus())
		if strings.Contains(got, " / ") {
			t.Errorf("FormatMetricsLine() = %q; want no session duration segment when StartTime is zero", got)
		}
	})

	t.Run("nonzero StartTime appends session duration with per-turn average", func(t *testing.T) {
		ts := fullMetricsStatus()
		ts.StartTime = ts.Timestamp.Add(-10 * time.Second)
		ts.CurrentTurns = 4 // turns = 5 → includes per-turn average
		got := FormatMetricsLine(ts)
		prefix := "[14:30:00] [deepseek-pro] M: 200 H: 800 C: 50 Th: 120 ($0.0012) [7.00s (ΣT: 1.50s)] / "
		if !strings.HasPrefix(got, prefix) {
			t.Errorf("FormatMetricsLine() = %q; want prefix %q", got, prefix)
		}
		// Tail: "<sessionDur>s (<sessionDur>/5)" — time.Since makes it non-deterministic,
		// so assert the structural shape instead of byte equality.
		tailRe := regexp.MustCompile(`^\d+\.\d{2}s \(\d+\.\d{2}\)$`)
		if !tailRe.MatchString(got[len(prefix):]) {
			t.Errorf("FormatMetricsLine() tail = %q; want shape <seconds>s (<seconds>)", got[len(prefix):])
		}
	})

	t.Run("negative CurrentTurns appends session duration without average", func(t *testing.T) {
		ts := fullMetricsStatus()
		ts.StartTime = ts.Timestamp.Add(-10 * time.Second)
		ts.CurrentTurns = -1 // turns = 0 → no per-turn average
		got := FormatMetricsLine(ts)
		prefix := "[14:30:00] [deepseek-pro] M: 200 H: 800 C: 50 Th: 120 ($0.0012) [7.00s (ΣT: 1.50s)] / "
		if !strings.HasPrefix(got, prefix) {
			t.Errorf("FormatMetricsLine() = %q; want prefix %q", got, prefix)
		}
		tailRe := regexp.MustCompile(`^\d+\.\d{2}s$`)
		if !tailRe.MatchString(got[len(prefix):]) {
			t.Errorf("FormatMetricsLine() tail = %q; want shape <seconds>s", got[len(prefix):])
		}
	})
}

func TestFormatFinalLine(t *testing.T) {
	t.Run("renders ready summary line", func(t *testing.T) {
		ts := events.TurnStatus{
			TaskCost:    0.0012,
			SessionCost: 0.1505,
			TotalM:      116386,
			TotalH:      15172096,
			TotalO:      51607,
		}
		got := FormatFinalLine(ts, 0.0010)
		want := "╰─⠿ Ready ($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096 99.2% O: 51607)"
		if got != want {
			t.Errorf("FormatFinalLine() = %q; want %q", got, want)
		}
	})

	t.Run("zero totals render zero hit rate", func(t *testing.T) {
		got := FormatFinalLine(events.TurnStatus{}, 0)
		want := "╰─⠿ Ready ($0.0000 $0.0000 $0.0000 M: 0 H: 0 0.0% O: 0)"
		if got != want {
			t.Errorf("FormatFinalLine() = %q; want %q", got, want)
		}
	})
}
