// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"io"
	"log/slog"
	"os"
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

// IsDebugEnabled reports whether the TELL_ME_DEBUG environment variable is
// set to "1", indicating a debug-mode request.
func IsDebugEnabled() bool {
	return os.Getenv("TELL_ME_DEBUG") == "1"
}
