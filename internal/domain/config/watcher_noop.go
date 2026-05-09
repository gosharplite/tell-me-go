// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// noOpConfigWatcher is a default ConfigWatcher that holds static values.
// It is used by the agent as a fallback when no watcher is injected via
// options — e.g. bare construction for tests.
type noOpConfigWatcher struct {
	mu               sync.RWMutex
	maxHistoryTokens int
	maxToolTurns     int
	maxHistoryTurns  int
	contextWindow    int
}

// NewNoOpConfigWatcher creates a ConfigWatcher with static default values.
func NewNoOpConfigWatcher(tokens, toolTurns, historyTurns int) ConfigWatcher {
	return &noOpConfigWatcher{
		maxHistoryTokens: tokens,
		maxToolTurns:     toolTurns,
		maxHistoryTurns:  historyTurns,
		contextWindow:    1000000,
	}
}

func (cw *noOpConfigWatcher) SetPaths(main, session string) {}
func (cw *noOpConfigWatcher) Refresh(model string)          {}

func (cw *noOpConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if tokens >= 0 {
		cw.maxHistoryTokens = tokens
	}
	if toolTurns >= 0 {
		cw.maxToolTurns = toolTurns
	}
	if historyTurns >= 0 {
		cw.maxHistoryTurns = historyTurns
	}
}

func (cw *noOpConfigWatcher) GetLimits() (int, int, int) {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.maxHistoryTokens, cw.maxToolTurns, cw.maxHistoryTurns
}

func (cw *noOpConfigWatcher) GetContextWindow() int {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.contextWindow
}

func (cw *noOpConfigWatcher) ApplyLimits(l events.Limits) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if l.MaxHistoryTokens >= 0 {
		cw.maxHistoryTokens = l.MaxHistoryTokens
	}
	if l.MaxToolTurns >= 0 {
		cw.maxToolTurns = l.MaxToolTurns
	}
	if l.MaxHistoryTurns >= 0 {
		cw.maxHistoryTurns = l.MaxHistoryTurns
	}
}
