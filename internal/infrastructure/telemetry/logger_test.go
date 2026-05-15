// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewSlogLogger_NilFallback(t *testing.T) {
	t.Parallel()

	logger := NewSlogLogger(nil)
	if logger == nil {
		t.Fatal("NewSlogLogger(nil) returned nil")
	}

	// Verify it delegates to slog.Default() by writing and checking output.
	// We can't easily capture slog.Default() output, but we can verify the type
	// is our slogLogger wrapper.
	sl, ok := logger.(*slogLogger)
	if !ok {
		t.Fatalf("expected *slogLogger, got %T", logger)
	}
	if sl.logger != slog.Default() {
		t.Error("expected logger to be slog.Default() when nil is passed")
	}
}

func TestNewSlogLogger_Custom(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	custom := slog.New(slog.NewTextHandler(&buf, nil))
	logger := NewSlogLogger(custom)

	if logger == nil {
		t.Fatal("NewSlogLogger(custom) returned nil")
	}

	sl, ok := logger.(*slogLogger)
	if !ok {
		t.Fatalf("expected *slogLogger, got %T", logger)
	}
	if sl.logger != custom {
		t.Error("expected logger to be the injected custom logger")
	}
}

func TestSlogLogger_Delegation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  func(logger *slogLogger, msg string)
		wantLvl string
		wantMsg string
	}{
		{
			name: "Error",
			method: func(l *slogLogger, msg string) {
				l.Error(msg)
			},
			wantLvl: "ERROR",
			wantMsg: "test error message",
		},
		{
			name: "Warn",
			method: func(l *slogLogger, msg string) {
				l.Warn(msg)
			},
			wantLvl: "WARN",
			wantMsg: "test warn message",
		},
		{
			name: "Info",
			method: func(l *slogLogger, msg string) {
				l.Info(msg)
			},
			wantLvl: "INFO",
			wantMsg: "test info message",
		},
		{
			name: "Debug",
			method: func(l *slogLogger, msg string) {
				l.Debug(msg)
			},
			wantLvl: "DEBUG",
			wantMsg: "test debug message",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			base := slog.New(handler)

			logger := &slogLogger{logger: base}
			tt.method(logger, tt.wantMsg)

			output := buf.String()
			if output == "" {
				t.Fatal("expected non-empty log output")
			}

			if !strings.Contains(output, "level="+tt.wantLvl) {
				t.Errorf("expected level %q in output, got: %s", tt.wantLvl, output)
			}
			if !strings.Contains(output, tt.wantMsg) {
				t.Errorf("expected message %q in output, got: %s", tt.wantMsg, output)
			}
		})
	}
}
