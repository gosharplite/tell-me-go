// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

func syncBridge(t *testing.T, b *Bridge, m *agenttest.MockUIRenderer) {
	t.Helper()
	done := make(chan struct{})
	prev := m.LogSystemMessageFn
	m.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {
		if prev != nil {
			prev(ctx, msg, level)
		}
		if msg == "SYNC_SENTINEL" {
			close(done)
		}
	}
	defer func() { m.LogSystemMessageFn = prev }()

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
