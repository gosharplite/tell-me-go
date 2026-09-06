// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package redirectwriter

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var discardTarget io.Writer = io.Discard

// Detacher defines the capability to detach a writer from its underlying output.
type Detacher interface {
	Detach() error
}

var (
	_ io.Writer = (*Writer)(nil)
	_ Detacher  = (*Writer)(nil)
)

// Writer wraps an underlying base io.Writer with an atomic target.
// Initially, writes pass through to base. Upon Detach(), base is flushed,
// closed (if io.Closer), and subsequent writes are atomically redirected to io.Discard.
type Writer struct {
	base      io.Writer
	target    atomic.Pointer[io.Writer]
	mu        sync.Mutex
	detached  bool
	detachErr error
}

// New constructs a new Writer wrapping base.
func New(base io.Writer) *Writer {
	if base == nil {
		base = io.Discard
	}
	w := &Writer{
		base: base,
	}
	w.target.Store(&w.base)
	return w
}

// Write writes bytes to the active atomic target.
func (w *Writer) Write(p []byte) (int, error) {
	target := *w.target.Load()
	return target.Write(p)
}

// Detach flushes base (if flusher/syncer), closes base (if io.Closer),
// and atomically swaps the target to io.Discard. It is thread-safe and idempotent.
func (w *Writer) Detach() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.detached {
		return w.detachErr
	}
	w.detached = true

	var errs []error

	if flusher, ok := w.base.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			errs = append(errs, err)
		}
	}

	if syncer, ok := w.base.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			errs = append(errs, err)
		}
	}

	if closer, ok := w.base.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	w.target.Store(&discardTarget)
	w.detachErr = errors.Join(errs...)
	return w.detachErr
}
