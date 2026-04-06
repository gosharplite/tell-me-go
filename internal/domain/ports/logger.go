// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// Logger defines the interface for logging within the application.
type Logger interface {
	Error(msg string, args ...any)
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}

// NoOpLogger is a logger that does nothing.
type NoOpLogger struct{}

func (l *NoOpLogger) Error(msg string, args ...any) {}
func (l *NoOpLogger) Warn(msg string, args ...any)  {}
func (l *NoOpLogger) Info(msg string, args ...any)  {}
func (l *NoOpLogger) Debug(msg string, args ...any) {}

type TurnsLogger interface {
	LogSystemMessage(msg string, level string, timestamp time.Time)
	LogTurnStatus(status events.TurnStatus, timestamp time.Time)
	Close() error
}

type NoOpTurnsLogger struct{}

func (l *NoOpTurnsLogger) LogSystemMessage(msg string, level string, timestamp time.Time) {}
func (l *NoOpTurnsLogger) LogTurnStatus(status events.TurnStatus, timestamp time.Time)    {}
func (l *NoOpTurnsLogger) Close() error                                                   { return nil }
