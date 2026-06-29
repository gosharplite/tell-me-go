// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
)

// TestLogTrace_WriteError_DevFull covers the f.Write failure branch in logTrace
// by writing to /dev/full, a device that accepts opens but fails writes with ENOSPC.
func TestLogTrace_WriteError_DevFull(t *testing.T) {
	if _, err := os.Stat("/dev/full"); os.IsNotExist(err) {
		t.Skip("/dev/full does not exist on this system")
	}

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}

	// Use a TraceLogger with production defaults.
	tl := newTraceLogger(slog.Default())
	tl.logTrace(context.Background(), "/dev/full", trace)

	// If we reach here without panicking, the write-error branch was exercised.
}

// errCloser wraps an io.Writer and returns an error on Close.
type errCloser struct {
	io.Writer
}

func (e *errCloser) Close() error {
	return errors.New("injected close error")
}

// TestLogTrace_CloseError_Mock verifies that when the injected openTraceFile
// returns an io.WriteCloser whose Close() method returns an error,
// logTrace logs a warning containing the error message.
func TestLogTrace_CloseError_Mock(t *testing.T) {
	t.Parallel()

	spy := &testfixtures.SpyLogger{}
	tl := &traceLogger{
		marshalFunc: json.Marshal,
		openTraceFile: func(path string) (io.WriteCloser, error) {
			return &errCloser{Writer: &bytes.Buffer{}}, nil
		},
		logger: newSpySlogLogger(spy),
	}

	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "trace.jsonl")

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}
	tl.logTrace(context.Background(), traceFile, trace)

	assert.True(t, spy.CalledWith("Warn", "failed to close trace file"),
		"expected slog.Warn 'failed to close trace file' to be logged")
}

// TestLogTrace_MarshalError verifies that when the injected marshalFunc
// returns an error, logTrace logs a warning and returns early.
func TestLogTrace_MarshalError(t *testing.T) {
	t.Parallel()

	spy := &testfixtures.SpyLogger{}
	tl := &traceLogger{
		marshalFunc: func(v interface{}) ([]byte, error) {
			return nil, errors.New("injected marshal error")
		},
		openTraceFile: newTraceLogger(nil).openTraceFile,
		logger:        newSpySlogLogger(spy),
	}

	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "trace.jsonl")

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}
	tl.logTrace(context.Background(), traceFile, trace)

	assert.True(t, spy.CalledWith("Warn", "failed to marshal TurnTrace"),
		"expected slog.Warn 'failed to marshal TurnTrace' to be logged")
}
