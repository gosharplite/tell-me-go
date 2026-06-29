// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

// TestMain redirects slog.Default() to a discard handler for all tests in
// this package. The telemetry package has numerous error-path tests that
// intentionally exercise slog.Warn code paths (corrupted files, permission
// errors, lock contention, etc.). Without this, those expected warnings
// flood the test harness output (testlog.txt), which can cause "file too
// large" errors during coverage-instrumented runs (issue #1167).
//
// Tests that need to assert on specific slog.Warn messages can use
// captureSlogOutput to temporarily swap in a spy handler.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// captureSlogOutput redirects slog.Default() to a spy handler backed by
// testfixtures.SpyLogger for the duration of the test. The returned spy
// can be used to assert that specific WARN/ERROR messages were emitted.
//
// Tests using this helper must NOT use t.Parallel() because
// slog.SetDefault is a global mutation.
func captureSlogOutput(t *testing.T) *testfixtures.SpyLogger {
	t.Helper()

	spy := &testfixtures.SpyLogger{}
	orig := slog.Default()
	slog.SetDefault(slog.New(&spySlogHandler{spy: spy, level: slog.LevelDebug}))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return spy
}

// spySlogHandler routes slog records to a testfixtures.SpyLogger for
// assertion.
type spySlogHandler struct {
	spy   *testfixtures.SpyLogger
	level slog.Level
}

func (h *spySlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *spySlogHandler) Handle(_ context.Context, r slog.Record) error {
	switch r.Level {
	case slog.LevelWarn:
		h.spy.Warn(r.Message)
	case slog.LevelError:
		h.spy.Error(r.Message)
	case slog.LevelInfo:
		h.spy.Info(r.Message)
	case slog.LevelDebug:
		h.spy.Debug(r.Message)
	}
	return nil
}

func (h *spySlogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *spySlogHandler) WithGroup(_ string) slog.Handler       { return h }
