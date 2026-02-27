// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"log"

	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
)

// TelemetryLogger implements the domaintools.ExecutionObserver interface using the global telemetry functions.
type TelemetryLogger struct{}

// ExecutionTimedOut logs a critical event when a tool goroutine leaks.
func (l *TelemetryLogger) ExecutionTimedOut(toolID string) {
	log.Printf("TELEMETRY CRITICAL: Tool goroutine permanently leaked: %s", toolID)
}

// ExecutionCompletedLate records a tool completion that occurred after its context was cancelled.
func (l *TelemetryLogger) ExecutionCompletedLate(toolID string) {
	// Note: We've lost the duration info here because the domain interface doesn't provide it.
	// For now we just record it with 0 or we could store the start time if we had access to it.
	// But according to the new ExecutionObserver interface, we only have toolID.
	telemetry.RecordLateCompletion(toolID, 0)
}
