// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"bytes"
	"context"
	"errors"
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

// TestFlush_CallerContextCancellation exercises the <-ctx.Done() branch in
// Flush's select (event_bus_lifecycle.go:108-109). The caller's context is
// pre-cancelled before Flush is called. Since pendingCount=1 keeps the
// flushWaiter blocked in cond.Wait() (~done never fires), and <-ctx.Done()
// is already ready, the select deterministically picks the cancellation
// branch. No goroutines, channels, or scheduler cooperation needed.
func TestFlush_CallerContextCancellation(t *testing.T) {
	t.Parallel()

	bus := NewSimpleEventBus(context.Background())
	bus.pendingCount = 1 // keep flushWaiter's loop alive (pendingCount > 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: <-ctx.Done() is immediately ready

	err := bus.Flush(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestFlush_BusShutdownDuringFlush exercises the <-b.ctx.Done() branch in
// Flush's select (event_bus_lifecycle.go:109-110). The bus's internal context
// is pre-cancelled before Flush is called. Since pendingCount=1 keeps the
// flushWaiter blocked in cond.Wait() (~done never fires), and <-b.ctx.Done()
// is already ready, the select deterministically picks the bus-cancellation
// branch. No goroutines, channels, or scheduler cooperation needed.
func TestFlush_BusShutdownDuringFlush(t *testing.T) {
	t.Parallel()

	bus := NewSimpleEventBus(context.Background())
	bus.pendingCount = 1 // keep flushWaiter's loop alive (pendingCount > 0)
	bus.cancel()         // pre-cancel: <-b.ctx.Done() is immediately ready

	err := bus.Flush(context.Background())
	if !errors.Is(err, ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

// TestShutdown_ContextCancellationDuringSelect exercises the <-ctx.Done()
// branch in Shutdown's select (event_bus_lifecycle.go:74-75). Unlike the
// pre-cancelled path (ctx.Err() guard at line 68-70, covered by
// TestSimpleEventBus_Shutdown_FlushTimeout), this test verifies that a context
// which is alive when Shutdown enters but expires while waiting for workers
// correctly returns the context error.
func TestShutdown_ContextCancellationDuringSelect(t *testing.T) {
	t.Parallel()

	bus := NewSimpleEventBus(context.Background(), WithAsync(true), WithQueueSize(2))

	// Start Listen with a cancellable context so we can clean up later.
	listenCtx, listenCancel := context.WithCancel(context.Background())
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = bus.Listen(listenCtx)
	}()
	bus.WaitStarted()

	// Subscribe a blocking subscriber that signals when its Handle method is
	// entered and then blocks until we release it. This keeps a worker
	// goroutine busy so waitWorkers never returns and done never closes.
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	bus.SubscribeGlobal(&funcSubscriber{f: func(ctx context.Context, e Event) {
		started <- struct{}{}
		<-block
	}})

	// Publish an event so a worker goroutine dequeues it, enters Handle,
	// and blocks on <-block.
	if err := bus.Publish(context.Background(), StatusUpdate{Message: "keep worker busy"}); err != nil {
		close(block)
		listenCancel()
		<-listenDone
		t.Fatalf("Publish failed: %v", err)
	}

	// Confirm the worker is blocked inside Handle.
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		close(block)
		listenCancel()
		<-listenDone
		t.Fatal("worker did not start processing")
	}

	// Call Shutdown with a context that has a short timeout. Inside Shutdown:
	//   - b.cancel() cancels the bus's internal context
	//   - waitWorkers goroutine starts but b.workerWG.Wait() won't return
	//     (worker is blocked on <-block)
	//   - done channel never closes
	//   - The early ctx.Err() check passes (context is not yet expired)
	//   - The select enters, <-done doesn't fire
	//   - After 50ms, <-ctx.Done() fires → the uncovered path
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shutdownCancel()
	err := bus.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded from ctx.Done() select branch, got %v", err)
	}

	// Cleanup: unblock the subscriber so the worker drains, then cancel the
	// listen context and wait for Listen to return.
	close(block)
	listenCancel()
	<-listenDone
}

// TestWaitStarted_NilBus exercises the nil-receiver early-return guard
// in WaitStarted. A nil SimpleEventBus must return immediately without
// panicking or blocking.
func TestWaitStarted_NilBus(t *testing.T) {
	t.Parallel()

	var bus *SimpleEventBus = nil

	panicCh := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
		}()
		bus.WaitStarted()
		close(done)
	}()

	select {
	case <-done:
		// Success — returned immediately without blocking
	case p := <-panicCh:
		t.Fatalf("panic on nil receiver: %v", p)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitStarted on nil bus blocked — nil guard may be missing")
	}
}

// TestSignalStarted_NilBus exercises the nil-receiver early-return guard
// in signalStarted. A nil SimpleEventBus must return immediately without
// panicking.
func TestSignalStarted_NilBus(t *testing.T) {
	t.Parallel()

	var bus *SimpleEventBus = nil

	panicCh := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
		}()
		bus.signalStarted()
		close(done)
	}()

	select {
	case <-done:
		// Success — returned immediately without blocking or panicking
	case p := <-panicCh:
		t.Fatalf("panic on nil receiver: %v", p)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("signalStarted on nil bus blocked — nil guard may be missing")
	}
}
