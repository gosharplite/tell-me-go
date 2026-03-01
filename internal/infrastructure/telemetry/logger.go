// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type slogLogger struct{}

// NewSlogLogger creates a new ports.Logger using the default slog handler.
func NewSlogLogger() ports.Logger {
	return &slogLogger{}
}

func (l *slogLogger) Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

func (l *slogLogger) Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

func (l *slogLogger) Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

func (l *slogLogger) Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}
