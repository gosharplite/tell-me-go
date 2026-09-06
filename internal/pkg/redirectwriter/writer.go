// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package redirectwriter

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"syscall"
)

var discardTarget io.Writer = io.Discard

// Detacher defines the capability to detach a writer from its underlying output.
type Detacher interface {
	Detach() error
}

// FdValuer defines the capability to retrieve a file descriptor.
type FdValuer interface {
	Fd() uintptr
}

// Unwrapper defines the capability to unwrap an underlying writer.
type Unwrapper interface {
	Unwrap() io.Writer
}

var (
	_ io.Writer = (*Writer)(nil)
	_ Detacher  = (*Writer)(nil)
	_ FdValuer  = (*Writer)(nil)
	_ Unwrapper = (*Writer)(nil)
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
		if err := syncer.Sync(); err != nil && !isIgnoredSyncError(err) {
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

// isIgnoredSyncError reports whether err indicates that the underlying stream
// does not support synchronization (e.g. pipes, FIFOs, sockets, or character devices).
// - Linux: fsync on a pipe/FIFO returns EINVAL (invalid argument).
// - Darwin (macOS): fcntl(fd, F_FULLFSYNC, 0) on non-vnodes (pipes, sockets) returns EBADF (bad file descriptor).
// - Other POSIX systems: fsync on unsupported descriptors may return ENOTSUP or ENOTTY.
func isIgnoredSyncError(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.EBADF) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.ENOTTY)
}

// Fd returns the underlying file descriptor if base implements interface{ Fd() uintptr }.
// When detached, or if base does not provide an FD, it returns ^uintptr(0).
func (w *Writer) Fd() uintptr {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.detached {
		return ^uintptr(0)
	}
	curr := w.base
	for curr != nil {
		if f, ok := curr.(interface{ Fd() uintptr }); ok {
			return f.Fd()
		}
		if u, ok := curr.(interface{ Unwrap() io.Writer }); ok {
			curr = u.Unwrap()
			continue
		}
		break
	}
	return ^uintptr(0)
}

// Unwrap returns the underlying base writer.
func (w *Writer) Unwrap() io.Writer {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.base
}
