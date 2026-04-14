// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"io"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

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

func (r *stdUIRenderer) SetGlamourRenderer(tr *glamour.TermRenderer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderer = tr
}

func (r *stdUIRenderer) GetMetricsProvider() ports.SystemMetricsProvider {
	return r.metricsProvider
}
