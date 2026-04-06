// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"io"
	"log/slog"
)

// NewLogger creates a configured slog.Logger based on the debug environment flag.
func NewLogger(stderr io.Writer, isDebug bool) *slog.Logger {
	logLevel := slog.LevelWarn
	if isDebug {
		logLevel = slog.LevelDebug
	}
	logHandler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level: logLevel,
	})
	return slog.New(logHandler)
}
