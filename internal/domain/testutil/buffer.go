// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// Buffer defines the contract for our thread-safe buffer.
type Buffer interface {
	io.Writer
	fmt.Stringer
	Reset()
	Len() int
	Bytes() []byte
}

var _ Buffer = (*SafeBuffer)(nil)

// SafeBuffer is a thread-safe wrapper around bytes.Buffer.
type SafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewSafeBuffer returns a new thread-safe buffer.
func NewSafeBuffer() Buffer {
	return &SafeBuffer{}
}

// Write appends the contents of p to the buffer, growing the buffer as needed.
func (s *SafeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// String returns the contents of the unread portion of the buffer as a string.
func (s *SafeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Reset resets the buffer to be empty.
func (s *SafeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

// Len returns the number of bytes of the unread portion of the buffer.
func (s *SafeBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

// Bytes returns a slice of length s.Len() holding the unread portion of the buffer.
func (s *SafeBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Bytes()
}

// SyncWriter is a writer that notifies a channel on every write.
type SyncWriter struct {
	mu      sync.Mutex
	Writer  io.Writer
	buf     bytes.Buffer
	OnWrite chan struct{}
}

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
