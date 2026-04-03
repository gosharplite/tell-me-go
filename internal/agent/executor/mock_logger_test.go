// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

// mockLogger is a mock implementation of the tools.ExecutionObserver interface for testing purposes.
type mockLogger struct {
	CriticalLogs chan string
}

// ExecutionTimedOut records a critical log message when a tool goroutine leaks.
func (m *mockLogger) ExecutionTimedOut(toolID string) {
	if m.CriticalLogs != nil {
		m.CriticalLogs <- "CRITICAL: Tool goroutine permanently leaked: " + toolID
	}
}

// ExecutionCompletedLate records a late completion event (not implemented in this mock).
func (m *mockLogger) ExecutionCompletedLate(toolID string) {
	// Not used in tests requiring verification
}
