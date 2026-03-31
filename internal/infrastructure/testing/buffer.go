// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

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

var _ Buffer = (*safeBuffer)(nil)

// safeBuffer is a thread-safe wrapper around bytes.Buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewSafeBuffer returns a new thread-safe buffer.
func NewSafeBuffer() Buffer {
	return &safeBuffer{}
}

// Write appends the contents of p to the buffer, growing the buffer as needed.
func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// String returns the contents of the unread portion of the buffer as a string.
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

// Bytes returns a slice of length s.Len() holding the unread portion of the buffer.
func (s *safeBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Bytes()
}
