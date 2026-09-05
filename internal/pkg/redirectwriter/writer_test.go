// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package redirectwriter_test

import (
	"bytes"
	"errors"
	"sync"
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
