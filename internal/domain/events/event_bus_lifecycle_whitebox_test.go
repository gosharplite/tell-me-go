// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// TestFlushWaiter_PanicRecovery exercises the recover() path in flushWaiter
// by causing sync.Cond.Wait to panic.
func TestFlushWaiter_PanicRecovery(t *testing.T) {
	t.Parallel()

	// Create a bus with a logger so we can verify panic log output.
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	bus := NewSimpleEventBus(context.Background(), WithLogger(log))

	// Corrupt the cond: replace the Locker with a nil locker so Wait() panics.
	// signalLocker also signals a channel when Wait() calls Unlock (just before
	// blocking), giving us a deterministic sync point instead of time.Sleep.
	entered := make(chan struct{})
	bus.pendingMu.Lock()
	bus.cond = sync.NewCond(&signalLocker{entered: entered})
	bus.pendingCount = 1 // Keep the loop alive so it enters cond.Wait
	bus.pendingMu.Unlock()

	done := make(chan struct{})
	cancelled := false

	go bus.flushWaiter(done, &cancelled)

	// Wait for flushWaiter to enter cond.Wait — cond.Wait calls Unlock()
	// just before blocking, which closes the entered channel.
	<-entered

	// Signal the cond to wake the waiter. When Wait() tries to re-acquire
	// the lock via signalLocker.Lock(), it panics — exercising the recover().
	bus.cond.Signal()

	// flushWaiter should recover from the panic and close(done).
	select {
	case <-done:
		// Success — flushWaiter recovered and closed done.
	case <-time.After(1 * time.Second):
		t.Fatal("flushWaiter did not close done within timeout — panic recovery likely failed")
	}

	// Verify the panic was logged.
	if !bytes.Contains(buf.Bytes(), []byte("panic in event bus flush wait")) {
		t.Errorf("expected 'panic in event bus flush wait' in log, got: %s", buf.String())
	}
}

// signalLocker implements sync.Locker: Lock panics (to test panic recovery),
// and Unlock signals a channel for deterministic synchronization in tests.
type signalLocker struct {
	entered chan struct{}
}

func (s *signalLocker) Lock()   { panic("nil locker") }
func (s *signalLocker) Unlock() { close(s.entered) }

// TestWaitWorkers_PanicRecovery exercises the recover() path in waitWorkers
// by replacing waitGroupWait with a panicking function.
func TestWaitWorkers_PanicRecovery(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	bus := NewSimpleEventBus(context.Background(), WithLogger(log))

	// Replace the wait function with one that panics.
	bus.wgWait = func(wg *sync.WaitGroup) { panic("injected worker wait panic") }

	done := make(chan struct{})
	go bus.waitWorkers(done)

	// waitWorkers should recover from the panic and close(done).
	select {
	case <-done:
		// Success — waitWorkers recovered and closed done.
	case <-time.After(1 * time.Second):
		t.Fatal("waitWorkers did not close done within timeout — panic recovery likely failed")
	}

	// Verify the panic was logged.
	if !bytes.Contains(buf.Bytes(), []byte("panic in event bus shutdown wait")) {
		t.Errorf("expected 'panic in event bus shutdown wait' in log, got: %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("injected worker wait panic")) {
		t.Errorf("expected 'injected worker wait panic' in log, got: %s", buf.String())
	}
}
