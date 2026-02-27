// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

func TestSimpleEventBus_Race(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	t.Cleanup(func() {
		if err := bus.Shutdown(context.Background()); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})
	wg := sync.WaitGroup{}

	numGoroutines := 10
	numEvents := 10

	wg.Add(numGoroutines * 2)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				bus.Subscribe(func(e events.Event) {})
			}
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				bus.Publish(struct{}{})
			}
		}()
	}

	wg.Wait()
}

func TestSimpleEventBus_DeterministicShutdown(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	count := 0
	mu := sync.Mutex{}

	bus.Subscribe(func(e events.Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	numEvents := 50
	for i := 0; i < numEvents; i++ {
		bus.Publish(i)
	}

	err := bus.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if count != numEvents {
		t.Errorf("Expected %d events, got %d", numEvents, count)
	}
}

func TestSimpleEventBus_Flush(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	t.Cleanup(func() {
		if err := bus.Shutdown(context.Background()); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})

	count := 0
	mu := sync.Mutex{}

	bus.Subscribe(func(e events.Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	numEvents := 50
	for i := 0; i < numEvents; i++ {
		bus.Publish(i)
	}

	err := bus.Flush(context.Background())
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if count != numEvents {
		t.Errorf("Expected %d events after flush, got %d", numEvents, count)
	}
}

func TestSimpleEventBus_Shutdown_ContextCancelled(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e events.Event) {
		ready <- struct{}{}
		<-block
	})
	bus.Publish("init")
	<-ready

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bus.Shutdown(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	close(block)
}

func TestSimpleEventBus_Flush_ClosedBus(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	err := bus.Flush(context.Background())
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestSimpleEventBus_Flush_ContextCancelled_Sending(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e events.Event) {
		ready <- struct{}{}
		<-block
	})
	bus.Publish("init")
	<-ready

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use a background goroutine to unblock the subscriber eventually.
	// But Flush should return immediately.
	defer close(block)

	err := bus.Flush(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSimpleEventBus_Flush_ContextCancelled_Waiting(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e events.Event) {
		ready <- struct{}{}
		<-block
	})
	bus.Publish("init")
	<-ready

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Unblock the subscriber ONLY after the context times out.
	go func() {
		<-ctx.Done()
		close(block)
	}()

	err := bus.Flush(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestSimpleEventBus_Subscribe_ClosedBus(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	bus.Subscribe(func(e events.Event) {
		t.Error("subscriber should not be called")
	})

	bus.Publish("event")
}

func TestSimpleEventBus_Publish_ClosedBus(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	// Should not return an error and not panic or block indefinitely
	bus.Publish("event")
}

func TestSimpleEventBus_Flush_NoSubscribers(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	err := bus.Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil error when flushing with no subscribers, got %v", err)
	}
}

func TestSimpleEventBus_BufferEviction(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	block := make(chan struct{})
	firstEvent := make(chan struct{})
	var received []events.Event
	var mu sync.Mutex
	var once sync.Once

	// Slow subscriber that intentionally starves the pumpEvents consumer
	bus.Subscribe(func(e events.Event) {
		once.Do(func() { close(firstEvent) })
		<-block // Block processing
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	// 1. Publish first event and wait for it to reach the subscriber
	bus.Publish("init")
	<-firstEvent

	// 2. Publish the remaining events in a tight loop. No runtime.Gosched() needed.
	// Publish 1500 events (exceeds 100 channel capacity + 1000 ring buffer capacity)
	// This ensures we trigger both the ring buffer eviction and the Publish channel drop.
	for i := 0; i < 1500; i++ {
		bus.Publish(i)
	}

	// Unblock subscriber and let it process
	close(block)

	// Wait for processing to finish
	bus.Flush(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// Because of channel capacities and ring buffer limits,
	// we shouldn't have received all 1501 events, proving eviction works and didn't panic.
	if len(received) == 1501 {
		t.Errorf("Expected events to be dropped/evicted, but received all 1501")
	}
	if len(received) == 0 {
		t.Errorf("Expected to receive some events, but got 0")
	}
}

func TestSimpleEventBus_Flush_ContextCancelled(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	block := make(chan struct{})
	firstEvent := make(chan struct{})
	var once sync.Once
	bus.Subscribe(func(e events.Event) {
		once.Do(func() { close(firstEvent) })
		<-block
	})

	// 1. Publish first event and wait for it to reach the subscriber
	bus.Publish("init")
	<-firstEvent

	// 2. Fill the ring buffer (1000) and the in channel (100)
	for i := 0; i < 1200; i++ {
		bus.Publish(i)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Subscriber will take time to unblock
	defer close(block)

	err := bus.Flush(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSimpleEventBus_Publish_BufferFull(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()
	block := make(chan struct{})

	// defer 1: Will execute LAST. Triggers graceful shutdown.
	defer func() {
		_ = bus.Shutdown(context.Background())
	}()

	// defer 2: Will execute FIRST. Unblocks the subscriber.
	defer close(block)

	bus.Subscribe(func(e events.Event) {
		<-block
	})

	for i := 0; i < 1200; i++ {
		bus.Publish(i)
	}
}

func TestSimpleEventBus_Flush_EvictedFlushEvent(t *testing.T) {
	t.Parallel()
	// Initialize a bus using NewSimpleEventBusWithCapacity(1).

	bus := events.NewSimpleEventBusWithCapacity(1)

	// Create a subscriber that blocks intentionally (so events queue up).
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e events.Event) {
		ready <- struct{}{}
		<-block
	})

	// 1. Publish first event and wait for it to reach the subscriber to block it
	bus.Publish("init")
	<-ready

	// 2. Now subscriber is blocked.
	// Publish an event that will occupy the 1-slot ring buffer.
	bus.Publish("in-buffer")

	// 3. Trigger Flush() in a separate goroutine. It will block.
	flushErr := make(chan error, 1)
	go func() {
		flushErr <- bus.Flush(context.Background())
	}()

	// Instead of Sleep, we use a loop with a small delay to ensure pumpEvents
	// has a chance to read the flushEvent from the 'in' channel.
	// We can't be 100% deterministic here without internal hooks, but this is
	// more robust than a single sleep.
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Millisecond)
		// 4. Publish one more event to force the ring buffer to overflow and evict the flushEvent.
		bus.Publish("force-eviction")

		select {
		case err := <-flushErr:
			if !errors.Is(err, events.ErrBufferOverflow) {
				t.Errorf("expected ErrBufferOverflow, got %v", err)
			}
			close(block)
			return
		default:
		}
	}

	close(block)
	t.Fatal("Flush did not return ErrBufferOverflow after multiple eviction attempts")
}

func TestSimpleEventBus_Flush_ConcurrentShutdown(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus()

	// Block processing to ensure Flush blocks.
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e events.Event) {
		ready <- struct{}{}
		<-block
	})

	bus.Publish("init")
	<-ready

	flushStarted := make(chan struct{})
	flushErr := make(chan error, 1)
	go func() {
		close(flushStarted)
		flushErr <- bus.Flush(context.Background())
	}()

	// Ensure Flush has actually reached its waiting state.
	<-flushStarted

	// Concurrently trigger bus.Shutdown() in a goroutine because it waits for subscribers.
	go func() {
		_ = bus.Shutdown(context.Background())
	}()

	// Assert that the Flush() call aborts and returns ErrBusClosed.
	select {
	case err := <-flushErr:
		if !errors.Is(err, events.ErrBusClosed) {
			t.Errorf("expected ErrBusClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Flush did not return after Shutdown")
	}

	close(block)
}

func TestSimpleEventBus_Flush_WaitsForAllToFinish(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBusWithCapacity(10)
	defer func() { _ = bus.Shutdown(context.Background()) }()

	var completed int32
	started := make(chan struct{})
	block := make(chan struct{})

	// Slow/hung subscriber
	bus.Subscribe(func(e events.Event) {
		close(started) // Signal to the test that this subscriber has picked up the event
		<-block        // Block indefinitely to simulate a hung process
		atomic.AddInt32(&completed, 1)
	})

	// Trigger an event to get the subscriber running
	bus.Publish("event")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Trigger the cancellation deterministically
	go func() {
		<-started // Wait until the subscriber is actually running and blocked
		cancel()  // Now cancel the context
	}()

	err := bus.Flush(ctx)

	// Cleanup to prevent the subscriber goroutine from leaking
	close(block)

	// 1. Assert Flush returned immediately due to context cancellation
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// 2. Assert Flush did not wait for the slow subscriber to finish
	if atomic.LoadInt32(&completed) != 0 {
		t.Errorf("Flush waited for the slow subscriber instead of returning immediately")
	}
}

func TestSimpleEventBus_DroppedEventsMetric(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBusWithCapacity(1)

	// Create a subscriber that blocks.
	block := make(chan struct{})
	bus.Subscribe(func(e events.Event) {
		<-block
	})

	// 1. Publish first event to occupy the subscriber.
	bus.Publish("first")

	// Wait a bit to ensure subscriber has picked up "first"
	// and is now blocked on the subscriber callback.
	time.Sleep(20 * time.Millisecond)

	// 2. Publish many events to saturate both the ring buffer and the 'in' channel.
	for i := 0; i < 50; i++ {
		bus.Publish(fmt.Sprintf("event-%d", i))
	}

	// Give pumpEvents a moment to pick up as many as possible and then block on 'out'.
	time.Sleep(20 * time.Millisecond)

	// 3. This event should definitely be dropped at the channel level.
	bus.Publish("dropped-1")

	if bus.DroppedEvents() == 0 {
		t.Errorf("expected dropped events at channel level, got 0")
	}

	close(block)
}
