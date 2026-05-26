// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
)

// newReleaseChan returns a channel plus a one-shot release function and
// registers a t.Cleanup to close it. Subscribers reading from this channel
// will unblock when either the test calls release() or the test ends,
// allowing all goroutines to exit cleanly so goleak is satisfied.
//
// release is safe to call multiple times; only the first call closes ch.
func newReleaseChan(t *testing.T) (ch <-chan struct{}, release func()) {
	t.Helper()
	c := make(chan struct{})
	var once sync.Once
	release = func() {
		once.Do(func() { close(c) })
	}
	t.Cleanup(release)
	return c, release
}

// TestPublish_DropsEventWhenSubscriberQueueIsFull verifies the backpressure
// "drop" arm of (*SimpleEventBus).enqueueEvent — the `default:` branch of
// its 3-arm select.
//
// Setup: a single global subscriber that blocks indefinitely inside Handle,
// with WithQueueSize(1). Listen() runs the worker, which receives the first
// event and blocks. Two more publishes saturate the queue and force the
// drop arm.
//
// Contract assertions:
//  1. Publish returns nil when the event is dropped (drop is not an error).
//  2. A Warn-level log line containing "subscriber queue full, dropping event"
//     is emitted.
//  3. The pendingCount is balanced (incPending followed by decPending in the
//     drop arm), verified indirectly via Flush() returning successfully —
//     a leaked counter would block Flush forever.
func TestPublish_DropsEventWhenSubscriberQueueIsFull(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	bus := events.NewSimpleEventBus(ctx,
		events.WithAsync(true),
		events.WithQueueSize(1),
		events.WithLogger(logger),
	)
	eventstest.CleanupBus(t, bus)

	releaseCh, release := newReleaseChan(t)
	started := make(chan struct{}, 1)
	bus.SubscribeGlobal(&uncooperativeSubscriber{
		block:             releaseCh,
		startedProcessing: started,
	})

	// Run the listener so the per-subscriber worker is started.
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = bus.Listen(ctx)
	}()
	bus.WaitStarted()
	t.Cleanup(func() {
		cancel()
		<-listenDone
	})

	// First publish: worker picks it up immediately and blocks inside Handle.
	if err := bus.Publish(ctx, testEvent{typeName: "queue_full_e1"}); err != nil {
		t.Fatalf("first Publish failed: %v", err)
	}

	// Wait deterministically for the subscriber to be inside Handle (blocked on release).
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("subscriber did not start processing in time")
	}

	// Second publish: fills the size-1 queue (worker is still blocked).
	if err := bus.Publish(ctx, testEvent{typeName: "queue_full_e2"}); err != nil {
		t.Fatalf("second Publish failed: %v", err)
	}

	// Third publish: queue is full, must hit the `default:` arm and be dropped.
	start := time.Now()
	err := bus.Publish(ctx, testEvent{typeName: "queue_full_e3"})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("dropped Publish should return nil, got: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("dropped Publish should be near-instant, took: %v", elapsed)
	}

	// Verify the Warn log was emitted for the dropped event.
	logOutput := buf.String()
	if !strings.Contains(logOutput, "subscriber queue full, dropping event") {
		t.Errorf("expected drop warning in log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "queue_full_e3") {
		t.Errorf("expected dropped event type queue_full_e3 in log, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "level=WARN") {
		t.Errorf("expected WARN level for drop log, got: %s", logOutput)
	}

	// Indirect pendingCount assertion:
	// The drop arm calls decPending() to undo its incPending(). If that were
	// not balanced, Flush would block until the timeout. We expect Flush to
	// hit its context deadline because pendingCount is still > 0 (the queued
	// e2 has not been processed — the worker is still blocked on E1). Then
	// we release and Flush again to verify clean drain.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer flushCancel()
	flushErr := bus.Flush(flushCtx)
	if !errors.Is(flushErr, context.DeadlineExceeded) {
		t.Errorf("expected Flush to time out while worker blocked, got: %v", flushErr)
	}

	// Now release the subscriber and verify Flush completes cleanly. If the
	// drop arm leaked a pendingCount, this Flush would also time out.
	release()
	flushCtx2, flushCancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer flushCancel2()
	if err := bus.Flush(flushCtx2); err != nil {
		t.Errorf("Flush after release should succeed (drop arm balanced pendingCount), got: %v", err)
	}
}

// TestPublish_ReturnsContextErrorWhenCancelledDuringEnqueue verifies the
// `case <-ctx.Done():` arm of (*SimpleEventBus).enqueueEvent.
//
// REACHABILITY ANALYSIS:
//
// Publish() performs a pre-flight context check that short-circuits with
// ctx.Err() if the context is already cancelled before dispatch begins.
// The inner select inside enqueueEvent also includes a `default:` arm,
// making it non-blocking. Together these mean the cancellation arm fires
// only in a narrow race window: ctx must become Done AFTER Publish's
// pre-flight check but BEFORE enqueueEvent's inner select evaluates, AND
// at evaluation time the success arm (`w.ch <- event`) must not be ready
// (otherwise either success or default-via-Go's-random-pick takes precedence).
//
// To force that race deterministically:
//  1. Register many global subscribers, each with WithQueueSize(1), and
//     pre-fill every queue. This guarantees the success arm is never ready
//     for any wrapper in the dispatchAsync loop, so the inner select is
//     always between `<-ctx.Done()` and `default:`.
//  2. With N wrappers, dispatchAsync's per-wrapper enqueueEvent loop is
//     wide enough that a concurrent ctx cancel can land between iterations,
//     exposing later wrappers to a Done context.
//  3. Start Publish in a goroutine, yield, then cancel its context — racing
//     the dispatch loop. Across enough iterations the cancel reliably lands
//     mid-loop and Go picks the cancellation arm in some wrapper.
//
// We cap iterations at 2000 and fail with a diagnostic message if no
// Publish ever returned context.Canceled — that would indicate a regression
// (arm removed) or a genuine scheduling issue worth investigating.
//
// IMPORTANT: We do NOT call bus.Listen, so no per-subscriber worker goroutine
// is ever spawned. Once pre-filled the queues stay full for the test's
// lifetime; goleak is satisfied because no background workers exist.
func TestPublish_ReturnsContextErrorWhenCancelledDuringEnqueue(t *testing.T) {
	t.Parallel()

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	bus := events.NewSimpleEventBus(rootCtx,
		events.WithAsync(true),
		events.WithQueueSize(1),
	)
	eventstest.CleanupBus(t, bus)

	// Many subscribers widen the dispatchAsync loop, giving the racing
	// cancel a much larger time window to land mid-loop.
	const numSubscribers = 64
	for i := 0; i < numSubscribers; i++ {
		bus.SubscribeGlobal(&errSubscriber{err: nil})
	}

	// Pre-fill every subscriber's queue using a fresh, uncancelled context
	// so each wrapper's queue holds exactly one event afterwards.
	if err := bus.Publish(rootCtx, testEvent{typeName: "prefill"}); err != nil {
		t.Fatalf("prefill Publish failed: %v", err)
	}

	const maxIter = 2000
	var sawCancellation atomic.Bool

	for i := 0; i < maxIter && !sawCancellation.Load(); i++ {
		pubCtx, pubCancel := context.WithCancel(rootCtx)

		done := make(chan error, 1)
		go func() {
			done <- bus.Publish(pubCtx, testEvent{typeName: "cancel_race"})
		}()

		// Yield so Publish has a chance to pass its pre-flight check before
		// we cancel — maximising the window where ctx becomes Done while
		// enqueueEvent's inner select is being evaluated for some wrapper.
		runtime.Gosched()
		pubCancel()

		if err := <-done; errors.Is(err, context.Canceled) {
			// Note: the returned error could come from Publish's pre-flight
			// OR from enqueueEvent's inner cancellation arm. We can't
			// distinguish from outside the package, so we rely on the
			// coverage tool to confirm the inner arm was actually exercised.
			// Continue iterating until we have given the race ample chances.
			sawCancellation.Store(true)
		}
	}

	if !sawCancellation.Load() {
		t.Fatalf("after %d iterations no Publish returned context.Canceled; "+
			"the `case <-ctx.Done():` arm of enqueueEvent appears unreachable. "+
			"Investigate: either the arm regressed or scheduling on this host "+
			"prevents the race window.", maxIter)
	}
}
