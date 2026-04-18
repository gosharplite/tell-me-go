// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

// MockLogger is a test double for tools.ExecutionObserver. When
// CriticalLogs is non-nil, ExecutionTimedOut emits a CRITICAL log line
// to the channel; this lets tests assert that goroutine-leak warnings
// are produced. ExecutionCompletedLate is a no-op.
type MockLogger struct {
	CriticalLogs chan string
}

func (m *MockLogger) ExecutionTimedOut(toolID string) {
	if m.CriticalLogs != nil {
		m.CriticalLogs <- "CRITICAL: Tool goroutine permanently leaked: " + toolID
	}
}

func (m *MockLogger) ExecutionCompletedLate(toolID string) {}
