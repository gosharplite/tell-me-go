// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

type uiErrorReader struct{}

func (e *uiErrorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestCaptureFromPipe_IOError(t *testing.T) {
	capturer := NewCapturer(&uiErrorReader{}, io.Discard, io.Discard, nil, nil, "", "", false).(*capturer)

	_, err := capturer.captureFromPipe(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read from pipe")
}

func TestCaptureFromTTY_IOError(t *testing.T) {
	capturer := NewCapturer(&uiErrorReader{}, io.Discard, io.Discard, nil, nil, "", "", false).(*capturer)

	_, err := capturer.captureFromTTY(context.Background(), false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read from TTY")
}
