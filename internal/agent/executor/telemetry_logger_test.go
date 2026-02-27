// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"testing"
)

func TestTelemetryLogger_ExecutionTimedOut(t *testing.T) {
	logger := &TelemetryLogger{}
	// This should not panic
	logger.ExecutionTimedOut("test-tool-id")
}

func TestTelemetryLogger_ExecutionCompletedLate(t *testing.T) {
	logger := &TelemetryLogger{}
	// This should not panic
	logger.ExecutionCompletedLate("test-tool-id")
}
