// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"io"
	"log/slog"
)

// NewLogger creates a configured slog.Logger based on the debug environment flag.
func NewLogger(stderr io.Writer, debugEnv string) *slog.Logger {
	logLevel := slog.LevelWarn
	if debugEnv == "1" {
		logLevel = slog.LevelDebug
	}
	logHandler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level: logLevel,
	})
	return slog.New(logHandler)
}
