// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// IsReaderBlocked returns true when readerMu is currently held by
// another goroutine. Used by tests to deterministically wait for a
// read goroutine to enter its critical section instead of using
// time.Sleep.
func (c *capturer) IsReaderBlocked() bool {
	if c.readerMu.TryLock() {
		c.readerMu.Unlock()
		return false
	}
	return true
}

// Export for testing
func RenderHistory(w io.Writer, h ports.HistoryManager, limit int, opts ports.HistoryRenderOptions) {
	renderHistory(w, h, limit, opts)
}

const (
	ColorBlue     = colorBlue
	TermClearLine = termClearLine
)

type (
	StdUIRenderer = stdUIRenderer
	MockLocker    = mockLocker
	MockClock     = mockClock
	UIState       = uiState
)

func NewMockLocker() *MockLocker {
	return &mockLocker{locked: false}
}

func NewMockClock(now time.Time) *MockClock {
	return &mockClock{now: now}
}

func (m *MockClock) SetNow(now time.Time) {
	m.now = now
}

func (m *MockClock) Add(d time.Duration) {
	m.now = m.now.Add(d)
}

func (r *stdUIRenderer) DrawLoadingIndicator(ui UIState, frame string, start time.Time, message string, showMetrics bool, m ports.SystemMetricsProvider) {
	r.drawLoadingIndicator(ui, frame, start, message, showMetrics, nil)
}

func (r *stdUIRenderer) ClearLoadingIndicator(ui UIState, force bool) {
	r.clearLoadingIndicator(ui, force)
}

func (r *stdUIRenderer) HandleSpinnerTick(ui UIState, frames []string, idx *int, start time.Time, status string, showMetrics bool, stopped *atomic.Bool) {
	r.handleSpinnerTick(ui, frames, idx, start, status, showMetrics, stopped)
}

func (r *stdUIRenderer) CleanupOnStop(ui UIState, stopped *atomic.Bool) {
	r.cleanupOnStop(ui, stopped)
}

func (r *stdUIRenderer) NowSafe() time.Time {
	return r.nowSafe()
}

func (r *stdUIRenderer) GetTimestamp() string {
	return r.getTimestamp()
}

func (r *stdUIRenderer) RenderMarkdown(text string) {
	r.renderMarkdown(text)
}

func (r *stdUIRenderer) GetUIState() UIState {
	return r.getUIState()
}

func (ui *UIState) C(s string) string {
	return ui.c(s)
}

func (r *stdUIRenderer) SetGlamourRenderer(tr markdownRenderer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderer = tr
}

func (r *stdUIRenderer) GetMetricsProvider() ports.SystemMetricsProvider {
	return r.metricsProvider
}
