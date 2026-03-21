// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type slogLogger struct {
	logger *slog.Logger
}

// NewSlogLogger creates a new ports.Logger using the provided slog.Logger.
// If logger is nil, it falls back to slog.Default().
func NewSlogLogger(l *slog.Logger) ports.Logger {
	if l == nil {
		l = slog.Default()
	}
	return &slogLogger{logger: l}
}

func (l *slogLogger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

func (l *slogLogger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

func (l *slogLogger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

func (l *slogLogger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}
