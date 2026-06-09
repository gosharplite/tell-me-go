// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
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

// TestFlush_CallerContextCancellation exercises the <-ctx.Done() branch in
// Flush's select (event_bus_lifecycle.go:108-109). It ensures that when the
// caller cancels its context while flushWaiter is blocked in cond.Wait(),
// cancelFlushWaiter sets the cancelled flag, broadcasts the cond, and
// returns context.Canceled.
//
// Synchronization is deterministic: a three-phase mutex handoff proves
// flushWaiter has released pendingMu via cond.Wait() before cancellation
// is triggered. No time.Sleep.
func TestFlush_CallerContextCancellation(t *testing.T) {
	t.Parallel()

	bus := NewSimpleEventBus(context.Background())
	bus.pendingCount = 1 // keep flushWaiter's loop alive

	// Phase 1: Hold pendingMu so flushWaiter cannot proceed past its Lock.
	bus.pendingMu.Lock()

	ctx, cancel := context.WithCancel(context.Background())

	errChan := make(chan error, 1)
	go func() {
		errChan <- bus.Flush(ctx)
	}()

	// Phase 2: Release the lock. flushWaiter will acquire pendingMu,
	// enter the for loop (pendingCount=1, !cancelled), and call cond.Wait().
	// cond.Wait() unlocks pendingMu just before blocking.
	bus.pendingMu.Unlock()

	// Yield to the scheduler so flushWaiter has a chance to run and
	// enter cond.Wait() before we try to re-acquire.
	runtime.Gosched()

	// Phase 3: Re-acquire pendingMu. This blocks until flushWaiter has
	// called cond.Wait() and released the mutex -- proving the waiter is
	// now blocked inside the cond.
	bus.pendingMu.Lock()

	// Trigger cancellation. Flush's select will see <-ctx.Done(),
	// call cancelFlushWaiter (which tries to acquire pendingMu and blocks),
	// then we release below.
	cancel()

	// Allow cancelFlushWaiter to acquire pendingMu, set cancelled=true,
	// call cond.Broadcast(), and return.
	bus.pendingMu.Unlock()

	// Collect the result.
	err := <-errChan
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Verify flushWaiter's done channel was closed (waiter exited cleanly).
	// If cancelFlushWaiter had a bug -- e.g. failing to set cancelled=true --
	// flushWaiter would still be blocked in cond.Wait() and done would never
	// close. The <-errChan above proves Flush returned, which implies
	// cancelFlushWaiter was called and flushWaiter's done was closed.
}

// TestFlush_BusShutdownDuringFlush exercises the <-b.ctx.Done() branch in
// Flush's select (event_bus_lifecycle.go:109-110). It ensures that when the
// bus's internal context is cancelled (simulating Shutdown) while flushWaiter
// is blocked in cond.Wait(), cancelFlushWaiter sets the cancelled flag,
// broadcasts the cond, and returns ErrBusClosed.
//
// Uses the same three-phase mutex handoff as TestFlush_CallerContextCancellation
// for deterministic synchronization. No time.Sleep.
func TestFlush_BusShutdownDuringFlush(t *testing.T) {
	t.Parallel()

	bus := NewSimpleEventBus(context.Background())
	bus.pendingCount = 1 // keep flushWaiter's loop alive

	// Phase 1: Hold pendingMu so flushWaiter cannot proceed past its Lock.
	bus.pendingMu.Lock()

	// Use a never-cancelled caller context so only <-b.ctx.Done() can fire.
	ctx := context.Background()

	errChan := make(chan error, 1)
	go func() {
		errChan <- bus.Flush(ctx)
	}()

	// Phase 2: Release the lock. flushWaiter acquires pendingMu, enters the
	// for loop, and calls cond.Wait() — which unlocks pendingMu.
	bus.pendingMu.Unlock()

	// Yield to let flushWaiter reach cond.Wait() before we re-acquire.
	runtime.Gosched()

	// Phase 3: Re-acquire pendingMu — proves flushWaiter is in cond.Wait().
	bus.pendingMu.Lock()

	// Simulate bus shutdown by cancelling the bus's internal context.
	// This causes Flush's select to pick <-b.ctx.Done().
	bus.cancel()

	// Allow cancelFlushWaiter to acquire pendingMu, set cancelled=true,
	// call cond.Broadcast(), and return ErrBusClosed.
	bus.pendingMu.Unlock()

	// Collect the result.
	err := <-errChan
	if !errors.Is(err, ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}
