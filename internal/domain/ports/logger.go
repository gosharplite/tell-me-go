// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

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
	HandleEvent(ctx context.Context, e events.Event)
	Close() error
}

type NoOpTurnsLogger struct{}

func (l *NoOpTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {}
func (l *NoOpTurnsLogger) Close() error                                    { return nil }
