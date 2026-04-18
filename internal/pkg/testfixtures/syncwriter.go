// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testfixtures

import (
	"bytes"
	"io"
	"sync"
)

// SyncWriter is a writer that notifies a channel on every Write call.
// It is useful in tests that need to deterministically wait for a
// goroutine to have written its output before proceeding (avoiding
// time.Sleep-based polling).
//
// SyncWriter is exported because consumers construct it as a struct
// literal (e.g. &testfixtures.SyncWriter{Writer: ..., OnWrite: ...}),
// which requires the type and its public fields to be accessible.
//
// Zero value is usable: writes go to an internal bytes.Buffer when
// Writer is nil, and OnWrite notifications are skipped when OnWrite is
// nil.
type SyncWriter struct {
	mu      sync.Mutex
	Writer  io.Writer
	buf     bytes.Buffer
	OnWrite chan struct{}
}

// Write writes p to the underlying writer (or the internal buffer if
// none was provided) and then performs a non-blocking send on OnWrite
// to notify any waiting goroutine. The send is dropped if OnWrite is
// full or nil.
func (w *SyncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var n int
	var err error
	if w.Writer != nil {
		n, err = w.Writer.Write(p)
	} else {
		n, err = w.buf.Write(p)
	}

	if w.OnWrite != nil {
		select {
		case w.OnWrite <- struct{}{}:
		default:
		}
	}
	return n, err
}

// String returns the accumulated output. If Writer was provided and
// itself exposes a String() method (e.g. *bytes.Buffer or the buffer
// returned by NewSafeBuffer), that string is returned; otherwise the
// internal buffer's contents are returned.
func (w *SyncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Writer != nil {
		if s, ok := w.Writer.(interface{ String() string }); ok {
			return s.String()
		}
	}
	return w.buf.String()
}
