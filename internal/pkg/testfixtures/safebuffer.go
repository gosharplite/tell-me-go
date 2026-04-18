// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package testfixtures provides cross-cutting test primitives that are
// useful across multiple unrelated packages but are too generic to belong
// in any single domain or layer. Examples include a thread-safe buffer
// and a channel-notifying writer used for goroutine synchronization in
// tests.
//
// Helpers in this package are intended only for use from _test.go files.
// Production code must never import this package.
package testfixtures

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// buffer is the unexported contract for a thread-safe buffer. Consumers
// receive it via the NewSafeBuffer constructor and program against this
// interface; the concrete type is intentionally unexported to keep the
// API surface area minimal.
type buffer interface {
	io.Writer
	fmt.Stringer
	Reset()
	Len() int
	Bytes() []byte
}

var _ buffer = (*safeBuffer)(nil)

// safeBuffer is the concrete thread-safe buffer implementation, returned
// by NewSafeBuffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewSafeBuffer returns a new thread-safe buffer suitable for capturing
// concurrent writes in tests (e.g. log output from multiple goroutines).
func NewSafeBuffer() buffer {
	return &safeBuffer{}
}

// Write appends the contents of p to the buffer, growing the buffer as
// needed.
func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// String returns the contents of the unread portion of the buffer as a
// string.
func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Reset resets the buffer to be empty.
func (s *safeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

// Len returns the number of bytes of the unread portion of the buffer.
func (s *safeBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

// Bytes returns a slice of length Len() holding the unread portion of
// the buffer.
func (s *safeBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Bytes()
}
