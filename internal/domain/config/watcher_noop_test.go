// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

func TestNoOpConfigWatcher_ConstructorAndGetLimits(t *testing.T) {
	cw := NewNoOpConfigWatcher(100, 10, 20)

	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 {
		t.Errorf("tokens = %d, want 100", tokens)
	}
	if toolTurns != 10 {
		t.Errorf("toolTurns = %d, want 10", toolTurns)
	}
	if historyTurns != 20 {
		t.Errorf("historyTurns = %d, want 20", historyTurns)
	}
}

func TestNoOpConfigWatcher_SetPathsAndRefresh(t *testing.T) {
	cw := NewNoOpConfigWatcher(100, 10, 20)

	// SetPaths should not panic
	cw.SetPaths("/some/main.yaml", "/some/session.json")

	// Refresh should not panic
	cw.Refresh("gpt-5")

	// Limits must be unchanged
	tokens, toolTurns, historyTurns := cw.GetLimits()
	if tokens != 100 || toolTurns != 10 || historyTurns != 20 {
		t.Errorf("limits changed after no-ops: got (%d, %d, %d), want (100, 10, 20)", tokens, toolTurns, historyTurns)
	}
}

func TestNoOpConfigWatcher_SetLimits(t *testing.T) {
	tests := []struct {
		name                                     string
		tokens, toolTurns, histTurns             int
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", 200, 5, 10, 200, 5, 10},
		{"zero tokens accepted", 0, 5, 10, 0, 5, 10},
		{"negative tokens ignored", -1, 5, 10, 100, 5, 10},
		{"mixed zero/positive", 200, 0, 10, 200, 0, 10},
		{"all zero accepted", 0, 0, 0, 0, 0, 0},
		{"partial update", 0, 0, 50, 0, 0, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := NewNoOpConfigWatcher(100, 10, 20)

			cw.SetLimits(tt.tokens, tt.toolTurns, tt.histTurns)
			tokens, toolTurns, histTurns := cw.GetLimits()

			if tokens != tt.wantTokens || toolTurns != tt.wantToolTurns || histTurns != tt.wantHistTurns {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tokens, toolTurns, histTurns,
					tt.wantTokens, tt.wantToolTurns, tt.wantHistTurns)
			}
		})
	}
}

func TestNoOpConfigWatcher_ApplyLimits(t *testing.T) {
	tests := []struct {
		name                                     string
		limits                                   events.Limits
		wantTokens, wantToolTurns, wantHistTurns int
	}{
		{"all positive", events.Limits{MaxHistoryTokens: 200, MaxToolTurns: 5, MaxHistoryTurns: 10}, 200, 5, 10},
		{"zero tokens accepted", events.Limits{MaxHistoryTokens: 0, MaxToolTurns: 5, MaxHistoryTurns: 10}, 0, 5, 10},
		{"negative tokens ignored", events.Limits{MaxHistoryTokens: -1, MaxToolTurns: 5, MaxHistoryTurns: 10}, 100, 5, 10},
		{"zero-value Limits accepted", events.Limits{}, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := NewNoOpConfigWatcher(100, 10, 20)

			cw.ApplyLimits(tt.limits)
			tokens, toolTurns, histTurns := cw.GetLimits()

			if tokens != tt.wantTokens || toolTurns != tt.wantToolTurns || histTurns != tt.wantHistTurns {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)",
					tokens, toolTurns, histTurns,
					tt.wantTokens, tt.wantToolTurns, tt.wantHistTurns)
			}
		})
	}
}

func TestNoOpConfigWatcher_RaceDetector(t *testing.T) {
	cw := NewNoOpConfigWatcher(100, 10, 20)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(v int) {
			for j := 0; j < 100; j++ {
				cw.SetLimits(v, v, v)
				_, _, _ = cw.GetLimits()
			}
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
