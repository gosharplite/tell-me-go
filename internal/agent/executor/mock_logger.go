// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

// MockLogger is a mock implementation of the domaintools.ExecutionObserver interface for testing purposes.
type MockLogger struct {
	CriticalLogs chan string
}

// ExecutionTimedOut records a critical log message when a tool goroutine leaks.
func (m *MockLogger) ExecutionTimedOut(toolID string) {
	if m.CriticalLogs != nil {
		m.CriticalLogs <- "CRITICAL: Tool goroutine permanently leaked: " + toolID
	}
}

// ExecutionCompletedLate records a late completion event (not implemented in this mock).
func (m *MockLogger) ExecutionCompletedLate(toolID string) {
	// Not used in tests requiring verification
}
