// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"io"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockHistoryRenderer is a mutex-guarded spy for ports.HistoryRenderer.
// It records every call to Render, allowing tests to inspect call counts
// and method names via Snapshot().  For a lightweight no-op stub that
// does not record anything, use StubHistoryRenderer from helpers.go.
type MockHistoryRenderer struct {
	mu            sync.Mutex
	renderCalls   int
	calledMethods []string
}

var _ ports.HistoryRenderer = (*MockHistoryRenderer)(nil)

func (m *MockHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	m.mu.Lock()
	m.renderCalls++
	m.calledMethods = append(m.calledMethods, "Render")
	m.mu.Unlock()
}

// Snapshot returns a consistent point-in-time copy of the accumulated
// call counters and method names.  It is safe to call from any goroutine.
func (m *MockHistoryRenderer) Snapshot() (renderCalls int, methods []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.renderCalls, out
}
