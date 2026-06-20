// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/stretchr/testify/assert"
)

// TestLogTrace_WriteError_DevFull covers the f.Write failure branch in logTrace
// by writing to /dev/full, a device that accepts opens but fails writes with ENOSPC.
func TestLogTrace_WriteError_DevFull(t *testing.T) {
	if _, err := os.Stat("/dev/full"); os.IsNotExist(err) {
		t.Skip("/dev/full does not exist on this system")
	}

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}

	// logTrace calls os.OpenFile(traceFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644).
	// On /dev/full:
	//   - O_CREATE is a no-op for device files.
	//   - O_APPEND|O_WRONLY succeeds.
	//   - The subsequent f.Write returns ENOSPC, hitting the target branch.
	logTrace(context.Background(), "/dev/full", trace)

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
	// NOT parallel — overrides package-level var
	originalOpen := openTraceFile
	openTraceFile = func(path string) (io.WriteCloser, error) {
		return &errCloser{Writer: &bytes.Buffer{}}, nil
	}
	t.Cleanup(func() { openTraceFile = originalOpen })

	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "trace.jsonl")

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}
	logTrace(context.Background(), traceFile, trace)

	assert.Contains(t, logBuf.String(), "Warning: Failed to close trace file")
	assert.Contains(t, logBuf.String(), "injected close error")
}

// TestLogTrace_MarshalError verifies that when the injected jsonMarshal
// function returns an error, logTrace logs a warning and returns early.
func TestLogTrace_MarshalError(t *testing.T) {
	// NOT parallel — overrides package-level var
	originalMarshal := jsonMarshal
	jsonMarshal = func(v interface{}) ([]byte, error) {
		return nil, errors.New("injected marshal error")
	}
	t.Cleanup(func() { jsonMarshal = originalMarshal })

	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "trace.jsonl")

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}
	logTrace(context.Background(), traceFile, trace)

	assert.Contains(t, logBuf.String(), "Warning: Failed to marshal TurnTrace")
	assert.Contains(t, logBuf.String(), "injected marshal error")
}
