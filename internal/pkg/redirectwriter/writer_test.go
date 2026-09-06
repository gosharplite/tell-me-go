// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package redirectwriter_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"sync"
	"syscall"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pkg/redirectwriter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockFlushSyncWriteCloser struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	flushed  int
	synced   int
	closed   int
	flushErr error
	syncErr  error
	closeErr error
	writeErr error
}

func (m *mockFlushSyncWriteCloser) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed > 0 {
		return 0, errors.New("write to closed writer")
	}
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return m.buf.Write(p)
}

func (m *mockFlushSyncWriteCloser) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return m.flushErr
}

func (m *mockFlushSyncWriteCloser) Sync() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.synced++
	return m.syncErr
}

func (m *mockFlushSyncWriteCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	return m.closeErr
}

func TestWriter_PreDetach(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := redirectwriter.New(&buf)

	n, err := w.Write([]byte("hello world"))
	require.NoError(t, err)
	assert.Equal(t, 11, n)
	assert.Equal(t, "hello world", buf.String())
}

func TestWriter_Detach_FlushesAndCloses(t *testing.T) {
	t.Parallel()
	mock := &mockFlushSyncWriteCloser{}
	w := redirectwriter.New(mock)

	err := w.Detach()
	require.NoError(t, err)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	assert.Equal(t, 1, mock.flushed, "Flush should be called once on Detach")
	assert.Equal(t, 1, mock.synced, "Sync should be called once on Detach")
	assert.Equal(t, 1, mock.closed, "Close should be called once on Detach")
}

func TestWriter_PostDetach_Discard(t *testing.T) {
	t.Parallel()
	mock := &mockFlushSyncWriteCloser{}
	w := redirectwriter.New(mock)

	n1, err1 := w.Write([]byte("pre-detach"))
	require.NoError(t, err1)
	assert.Equal(t, 10, n1)

	errDetach := w.Detach()
	require.NoError(t, errDetach)

	// Subsequent write after detach goes to io.Discard without error and without touching closed base
	n2, err2 := w.Write([]byte("post-detach"))
	require.NoError(t, err2)
	assert.Equal(t, 11, n2)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	assert.Equal(t, "pre-detach", mock.buf.String())
	assert.Equal(t, 1, mock.closed)
}

func TestWriter_Detach_Idempotent(t *testing.T) {
	t.Parallel()
	mock := &mockFlushSyncWriteCloser{}
	w := redirectwriter.New(mock)

	for i := 0; i < 5; i++ {
		err := w.Detach()
		require.NoError(t, err)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	assert.Equal(t, 1, mock.flushed, "Flush should only be called on first Detach")
	assert.Equal(t, 1, mock.synced, "Sync should only be called on first Detach")
	assert.Equal(t, 1, mock.closed, "Close should only be called on first Detach")
}

func TestWriter_Concurrent(t *testing.T) {
	t.Parallel()
	mock := &mockFlushSyncWriteCloser{}
	w := redirectwriter.New(mock)

	var wg sync.WaitGroup
	const writers = 20
	const detachCalls = 5
	const iterations = 100

	// Launch concurrent writers
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := []byte("data")
			for j := 0; j < iterations; j++ {
				_, _ = w.Write(data)
			}
		}()
	}

	// Launch concurrent detach callers
	for i := 0; i < detachCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.Detach()
		}()
	}

	wg.Wait()

	// Verify post-condition: writer is detached and subsequent write succeeds
	n, err := w.Write([]byte("final"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	assert.Equal(t, 1, mock.closed)
}

func TestWriter_NilBase(t *testing.T) {
	t.Parallel()
	w := redirectwriter.New(nil)

	n, err := w.Write([]byte("payload"))
	require.NoError(t, err)
	assert.Equal(t, 7, n)

	err = w.Detach()
	require.NoError(t, err)
}

func TestWriter_Detach_ErrorPropagation(t *testing.T) {
	t.Parallel()
	flushErr := errors.New("flush failed")
	syncErr := errors.New("sync failed")
	closeErr := errors.New("close failed")

	mock := &mockFlushSyncWriteCloser{
		flushErr: flushErr,
		syncErr:  syncErr,
		closeErr: closeErr,
	}
	w := redirectwriter.New(mock)

	err := w.Detach()
	require.Error(t, err)
	assert.ErrorIs(t, err, flushErr)
	assert.ErrorIs(t, err, syncErr)
	assert.ErrorIs(t, err, closeErr)

	// Idempotent error return
	err2 := w.Detach()
	assert.Equal(t, err, err2)
}
func TestWriter_Fd(t *testing.T) {
	t.Parallel()

	t.Run("pre-detach with os.File", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		defer func() {
			_ = pr.Close()
			_ = pw.Close()
		}()

		w := redirectwriter.New(pw)
		assert.Equal(t, pw.Fd(), w.Fd())
	})

	t.Run("post-detach returns invalid FD", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		defer func() {
			_ = pr.Close()
			_ = pw.Close()
		}()

		w := redirectwriter.New(pw)
		require.NoError(t, w.Detach())
		assert.Equal(t, ^uintptr(0), w.Fd())
	})

	t.Run("non-FD base returns invalid FD", func(t *testing.T) {
		var buf bytes.Buffer
		w := redirectwriter.New(&buf)
		assert.Equal(t, ^uintptr(0), w.Fd())
	})

	t.Run("nil base returns invalid FD", func(t *testing.T) {
		w := redirectwriter.New(nil)
		assert.Equal(t, ^uintptr(0), w.Fd())
	})

	t.Run("nested wrappers resolve FD", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		defer func() {
			_ = pr.Close()
			_ = pw.Close()
		}()

		w1 := redirectwriter.New(pw)
		w2 := redirectwriter.New(w1)
		assert.Equal(t, pw.Fd(), w2.Fd())

		// When inner w1 is detached, w2.Fd() returns invalid FD
		require.NoError(t, w1.Detach())
		assert.Equal(t, ^uintptr(0), w2.Fd())
	})

	t.Run("nested wrappers outer detached", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		defer func() {
			_ = pr.Close()
			_ = pw.Close()
		}()

		w1 := redirectwriter.New(pw)
		w2 := redirectwriter.New(w1)
		require.NoError(t, w2.Detach())
		assert.Equal(t, ^uintptr(0), w2.Fd())
	})
}

func TestWriter_Unwrap(t *testing.T) {
	t.Parallel()

	t.Run("returns underlying base", func(t *testing.T) {
		var buf bytes.Buffer
		w := redirectwriter.New(&buf)
		assert.Same(t, &buf, w.Unwrap())
	})

	t.Run("concurrency with Fd, Unwrap, Write, and Detach", func(t *testing.T) {
		pr, pw, err := os.Pipe()
		require.NoError(t, err)
		defer func() {
			_ = pr.Close()
			_ = pw.Close()
		}()

		w := redirectwriter.New(pw)

		var wg sync.WaitGroup
		const callers = 10
		const iterations = 50

		for i := 0; i < callers; i++ {
			wg.Add(4)
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = w.Fd()
				}
			}()
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_ = w.Unwrap()
				}
			}()
			go func() {
				defer wg.Done()
				for j := 0; j < iterations; j++ {
					_, _ = w.Write([]byte("concurrent"))
				}
			}()
			go func() {
				defer wg.Done()
				_ = w.Detach()
			}()
		}

		wg.Wait()
		assert.Equal(t, ^uintptr(0), w.Fd())
	})
}

type stubSyncer struct {
	syncErr error
}

func (s *stubSyncer) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *stubSyncer) Sync() error {
	return s.syncErr
}

func TestWriter_Detach_SyncIgnoredErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		syncErr error
		wantErr bool
	}{
		{
			name:    "PathError wrapping syscall.EINVAL",
			syncErr: &fs.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.EINVAL},
			wantErr: false,
		},
		{
			name:    "direct syscall.EINVAL",
			syncErr: syscall.EINVAL,
			wantErr: false,
		},
		{
			name:    "PathError wrapping syscall.EBADF (Darwin pipe/stream)",
			syncErr: &fs.PathError{Op: "sync", Path: "|1", Err: syscall.EBADF},
			wantErr: false,
		},
		{
			name:    "direct syscall.EBADF",
			syncErr: syscall.EBADF,
			wantErr: false,
		},
		{
			name:    "PathError wrapping syscall.ENOTSUP",
			syncErr: &fs.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.ENOTSUP},
			wantErr: false,
		},
		{
			name:    "direct syscall.ENOTSUP",
			syncErr: syscall.ENOTSUP,
			wantErr: false,
		},
		{
			name:    "PathError wrapping syscall.ENOTTY",
			syncErr: &fs.PathError{Op: "sync", Path: "/dev/stdout", Err: syscall.ENOTTY},
			wantErr: false,
		},
		{
			name:    "direct syscall.ENOTTY",
			syncErr: syscall.ENOTTY,
			wantErr: false,
		},
		{
			name:    "non-ignored sync error is retained",
			syncErr: syscall.EIO,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := redirectwriter.New(&stubSyncer{
				syncErr: tt.syncErr,
			})
			err := w.Detach()
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.syncErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
