// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Benchmark helper types
// ---------------------------------------------------------------------------

// benchEvent is a minimal zero-allocation Event implementation for benchmarks.
// Using the zero-value benchEvent{} costs nothing — no heap allocation, no
// field initialization — unlike testEvent{typeName: "bench"} which allocates a
// string header on every iteration.
type benchEvent struct{}

func (e benchEvent) Type() string { return "benchEvent" }

// noopSubscriber is a zero-cost Subscriber whose Handle returns nil
// immediately. Useful for measuring raw throughput of the bus without any
// subscriber work.
type noopSubscriber struct{}

func (s *noopSubscriber) Handle(_ context.Context, _ Event) error { return nil }

// blockingSubscriber is a Subscriber that blocks until explicitly released
// or the context is cancelled. It respects context cancellation so the 30s
// subscriberLoop timeout never fires during cleanup (avoiding log allocation
// noise in benchmarks).
//
// An optional started channel can be provided to signal (non-blocking) when
// Handle is entered. This allows benchmarks to deterministically wait for the
// worker goroutine without time.Sleep.
type blockingSubscriber struct {
	release <-chan struct{}
	started chan<- struct{} // optional: signaled when Handle is entered
}

func (s *blockingSubscriber) Handle(ctx context.Context, _ Event) error {
	// Signal that we've started blocking (non-blocking send, best-effort)
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// Benchmark helper functions
// ---------------------------------------------------------------------------

// startAsyncBus launches a SimpleEventBus with an async listener goroutine and
// wires up cleanup via b.Cleanup. It returns the cancel function for the listen
// context so benchmarks can signal early shutdown if needed.
//
// Cleanup sequence:
//  1. Cancels the listen context.
//  2. Waits for the Listen goroutine to return.
//  3. Calls bus.Shutdown with a 2s timeout.
func startAsyncBus(tb testing.TB, bus *SimpleEventBus) context.CancelFunc {
	tb.Helper()

	// Discard logger to avoid allocation noise from subscriber timeouts / panics
	// contaminating benchmark metrics.
	bus.log = slog.New(slog.NewJSONHandler(io.Discard, nil))

	listenCtx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = bus.Listen(listenCtx)
	}()

	bus.WaitStarted()

	tb.Cleanup(func() {
		cancel()
		wg.Wait()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = bus.Shutdown(shutdownCtx)
	})

	return cancel
}

// BenchmarkEventBusPublish measures sync-mode Publish throughput for two
// scenarios: (1) zero subscribers — pure overhead of the publish pipeline;
// (2) one no-op subscriber — full dispatch path including subscriber
// notification but zero subscriber work. Async mode is explicitly disabled
// to eliminate goroutine scheduling noise, and logging is sent to io.Discard
// to avoid I/O allocation contamination.
func BenchmarkEventBusPublish(b *testing.B) {
	b.Run("noSub", func(b *testing.B) {
		bus := NewSimpleEventBus(context.Background(),
			WithAsync(false),
			WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		)
		b.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = bus.Shutdown(ctx)
		})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bus.Publish(context.Background(), benchEvent{})
		}
	})

	b.Run("oneSub", func(b *testing.B) {
		bus := NewSimpleEventBus(context.Background(),
			WithAsync(false),
			WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		)
		bus.SubscribeSubscriber(benchEvent{}.Type(), &noopSubscriber{})
		b.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = bus.Shutdown(ctx)
		})
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = bus.Publish(context.Background(), benchEvent{})
		}
	})
}

// BenchmarkEventBusPublishParallel measures async-mode Publish throughput when
// dispatching to N subscribers via dispatchAsync. Each subscriber gets its own
// buffered channel (defaultQueueSize = 1024) and worker goroutine. With
// noopSubscriber handlers, the channel send is the dominant cost — workers
// drain instantly, so backpressure never triggers.
//
// Subtests: subs=1, subs=8, subs=64.
func BenchmarkEventBusPublishParallel(b *testing.B) {
	subCounts := []int{1, 8, 64}

	for _, n := range subCounts {
		b.Run(subName(n), func(b *testing.B) {
			bus := NewSimpleEventBus(context.Background(),
				WithAsync(true),
				WithQueueSize(defaultQueueSize),
			)

			for i := 0; i < n; i++ {
				bus.SubscribeSubscriber(benchEvent{}.Type(), &noopSubscriber{})
			}

			_ = startAsyncBus(b, bus)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = bus.Publish(context.Background(), benchEvent{})
			}
			b.StopTimer()
		})
	}
}

// subName returns a subtests name like "subs=8".
func subName(n int) string {
	if n == 1 {
		return "subs=1"
	}
	if n == 8 {
		return "subs=8"
	}
	return "subs=64"
}

// BenchmarkEventBusBackpressure measures the ns/op of Publish when a
// subscriber's queue is saturated — i.e., the default: (drop) arm of the
// 3-way select in enqueueEvent. This includes: incPending → 3-way select
// (success arm not ready, ctx not done, falls to default) → decPending →
// log.Warn argument evaluation (no I/O since logger is discarded).
//
// Coordination (deterministic, no time.Sleep):
//  1. Create a blockingSubscriber with a size-1 queue and a started signal.
//  2. Publish event 1 → worker dequeues it, enters Handle, signals started,
//     blocks on release.
//  3. Wait for started → guarantees the worker is now inside Handle.
//  4. Publish event 2 → fills the size-1 queue.
//  5. Queue is now saturated. Every subsequent Publish hits the drop path.
//  6. Cleanup is handled by startAsyncBus: cancel context → Listen returns →
//     Shutdown. The blockingSubscriber respects ctx.Done(), so it unblocks
//     when the listen context is cancelled.
func BenchmarkEventBusBackpressure(b *testing.B) {
	started := make(chan struct{}, 2) // buffered so non-blocking send in Handle never blocks
	release := make(chan struct{})

	bus := NewSimpleEventBus(context.Background(),
		WithAsync(true),
		WithQueueSize(1),
	)

	bus.SubscribeSubscriber(benchEvent{}.Type(), &blockingSubscriber{
		release: release,
		started: started,
	})

	_ = startAsyncBus(b, bus)

	// Publish event 1 — worker dequeues it, enters Handle, signals started, blocks on release.
	_ = bus.Publish(context.Background(), benchEvent{})
	<-started // guarantees the worker is now blocked inside Handle

	// Publish event 2 — fills the size-1 queue (worker is still blocked on event 1).
	_ = bus.Publish(context.Background(), benchEvent{})

	// Queue is saturated. Every subsequent Publish hits the drop path.

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bus.Publish(context.Background(), benchEvent{})
	}
	b.StopTimer()
}
