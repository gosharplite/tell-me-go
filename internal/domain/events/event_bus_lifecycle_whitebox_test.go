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
	// This is a white-box technique — only safe in-package.
	bus.pendingMu.Lock()
	bus.cond = sync.NewCond((*nilLocker)(nil))
	bus.pendingCount = 1 // Keep the loop alive so it enters cond.Wait
	bus.pendingMu.Unlock()

	done := make(chan struct{})
	cancelled := false

	go bus.flushWaiter(done, &cancelled)

	// Give the goroutine time to enter cond.Wait.
	time.Sleep(50 * time.Millisecond)

	// Signal the cond to wake the waiter. When Wait() tries to re-acquire
	// the lock via nilLocker.Lock(), it panics — exercising the recover().
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

// nilLocker implements sync.Locker but panics on Lock.
type nilLocker struct{}

func (n *nilLocker) Lock()   { panic("nil locker") }
func (n *nilLocker) Unlock() {}

// TestWaitWorkers_PanicRecovery exercises the recover() path in waitWorkers
// by replacing waitGroupWait with a panicking function.
func TestWaitWorkers_PanicRecovery(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	bus := NewSimpleEventBus(context.Background(), WithLogger(log))

	// Replace the wait function with one that panics.
	origWait := waitGroupWait
	waitGroupWait = func(wg *sync.WaitGroup) { panic("injected worker wait panic") }
	defer func() { waitGroupWait = origWait }()

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
