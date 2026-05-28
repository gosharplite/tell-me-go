// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// Logger defines the interface for structured logging within the application.
// Implementations should support key=value pairs as variadic args
// (e.g., logger.Info("message", "key1", val1, "key2", val2)).
type Logger interface {
	// Error logs a message at ERROR level. Used for unrecoverable
	// failures that require operator attention.
	Error(msg string, args ...any)

	// Warn logs a message at WARN level. Used for potentially harmful
	// situations that do not prevent continued operation.
	Warn(msg string, args ...any)

	// Info logs a message at INFO level. Used for significant events
	// during normal operation (e.g., startup, completion).
	Info(msg string, args ...any)

	// Debug logs a message at DEBUG level. Used for diagnostic
	// information useful during development and troubleshooting.
	Debug(msg string, args ...any)
}

// NoOpLogger is a logger that does nothing.
type NoOpLogger struct{}

func (l *NoOpLogger) Error(msg string, args ...any) {}
func (l *NoOpLogger) Warn(msg string, args ...any)  {}
func (l *NoOpLogger) Info(msg string, args ...any)  {}
func (l *NoOpLogger) Debug(msg string, args ...any) {}

// TurnsLogger defines the interface for logging conversation turns to disk.
type TurnsLogger interface {
	// HandleEvent processes a single event synchronously. It is safe
	// to call from any goroutine. Events are buffered and flushed
	// asynchronously by the background workers started in Listen.
	HandleEvent(ctx context.Context, e events.Event)

	// Listen starts the turns logger's background workers and blocks
	// until the context is canceled.
	// [ARCHITECTURAL REFACTOR] This replaces the previous fire-and-forget goroutine pattern.
	Listen(ctx context.Context) error

	// Close flushes any buffered events and releases resources.
	// It must be called after Listen returns.
	Close() error
}

// NoOpTurnsLogger is a TurnsLogger that discards all events.
// It is used when turn logging is disabled or unavailable.
type NoOpTurnsLogger struct{}

func (l *NoOpTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {}
func (l *NoOpTurnsLogger) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }
func (l *NoOpTurnsLogger) Close() error                                    { return nil }
