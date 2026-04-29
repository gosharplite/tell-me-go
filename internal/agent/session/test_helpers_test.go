// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/mock"
)

func syncBridge(t *testing.T, b *UIBridge, m interface {
	On(methodName string, arguments ...interface{}) *mock.Call
}) {
	t.Helper()
	// Use a sentinel event that is handled by the bridge and calls a mock method.
	// LogSystemMessage is ideal as it's safe to call when no spinner is active.
	done := make(chan struct{})
	m.On("LogSystemMessage", mock.Anything, "SYNC_SENTINEL", "info").Run(func(_ mock.Arguments) {
		close(done)
	}).Return().Once()

	// Use a non-polling send via HandleEvent. SystemMessageEvent is critical
	// and will be delivered with backpressure.
	if err := b.HandleEvent(context.Background(), events.SystemMessageEvent{Message: "SYNC_SENTINEL", Level: "info"}); err != nil {
		t.Fatalf("Failed to queue sync sentinel: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for sync sentinel processing")
	}
}

type syncWriter struct {
	mu      sync.Mutex
	Writer  io.Writer
	buf     bytes.Buffer
	onWrite chan struct{}
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var n int
	var err error
	if w.Writer != nil {
		n, err = w.Writer.Write(p)
	} else {
		n, err = w.buf.Write(p)
	}

	if w.onWrite != nil {
		select {
		case w.onWrite <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Writer != nil {
		if s, ok := w.Writer.(interface{ String() string }); ok {
			return s.String()
		}
	}
	return w.buf.String()
}

func (w *syncWriter) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Writer != nil {
		if r, ok := w.Writer.(interface{ Reset() }); ok {
			r.Reset()
		}
	}
	w.buf.Reset()
}
