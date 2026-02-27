// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"log"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
)

// TelemetryLogger implements the domaintools.Logger interface using the global telemetry functions.
type TelemetryLogger struct{}

// LogCritical logs a critical event to the standard logger with a TELEMETRY prefix.
func (l *TelemetryLogger) LogCritical(msg string) {
	log.Printf("TELEMETRY %s", msg)
}

// RecordLateCompletion records a tool completion that occurred after its context was cancelled.
func (l *TelemetryLogger) RecordLateCompletion(name string, d time.Duration) {
	telemetry.RecordLateCompletion(name, d)
}
