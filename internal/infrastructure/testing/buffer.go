// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

var (
	_ io.Writer    = (*SafeBuffer)(nil)
	_ fmt.Stringer = (*SafeBuffer)(nil)
)

// SafeBuffer is a thread-safe wrapper around bytes.Buffer.
type SafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
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
