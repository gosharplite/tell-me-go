// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"time"
)

// MockLogger is a mock implementation of the domaintools.Logger interface for testing purposes.
type MockLogger struct {
	CriticalLogs chan string
}

// LogCritical records a critical log message to the CriticalLogs channel.
func (m *MockLogger) LogCritical(msg string) {
	if m.CriticalLogs != nil {
		m.CriticalLogs <- msg
	}
}

// RecordLateCompletion records a late completion event (not implemented in this mock).
func (m *MockLogger) RecordLateCompletion(name string, d time.Duration) {
	// Not used in tests requiring verification
}
