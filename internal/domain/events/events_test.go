// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSimpleEventBus_Race(t *testing.T) {
	bus := NewSimpleEventBus()
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
				bus.Subscribe(func(e Event) {})
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
	bus := NewSimpleEventBus()
	count := 0
	mu := sync.Mutex{}

	bus.Subscribe(func(e Event) {
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
	bus := NewSimpleEventBus()
	t.Cleanup(func() {
		if err := bus.Shutdown(context.Background()); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	})

	count := 0
	mu := sync.Mutex{}

	bus.Subscribe(func(e Event) {
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
	bus := NewSimpleEventBus()
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e Event) {
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
	bus := NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	err := bus.Flush(context.Background())
	if err == nil || err.Error() != "event bus is closed" {
		t.Errorf("expected 'event bus is closed' error, got %v", err)
	}
}

func TestSimpleEventBus_Flush_ContextCancelled_Sending(t *testing.T) {
	bus := NewSimpleEventBus()
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e Event) {
		ready <- struct{}{}
		<-block
	})
	bus.Publish("init")
	<-ready

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bus.Flush(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	close(block)
}

func TestSimpleEventBus_Flush_ContextCancelled_Waiting(t *testing.T) {
	bus := NewSimpleEventBus()
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e Event) {
		ready <- struct{}{}
		<-block
	})
	bus.Publish("init")
	<-ready

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := bus.Flush(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	close(block)
}

func TestSimpleEventBus_Subscribe_ClosedBus(t *testing.T) {
	bus := NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	bus.Subscribe(func(e Event) {
		t.Error("subscriber should not be called")
	})

	bus.Publish("event")
}

func TestSimpleEventBus_Publish_ClosedBus(t *testing.T) {
	bus := NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	// Should not return an error and not panic or block indefinitely
	bus.Publish("event")
}

func TestSimpleEventBus_Flush_NoSubscribers(t *testing.T) {
	bus := NewSimpleEventBus()
	err := bus.Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil error when flushing with no subscribers, got %v", err)
	}
}

func TestSimpleEventBus_BufferEviction(t *testing.T) {
	bus := NewSimpleEventBus()
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	block := make(chan struct{})
	firstEvent := make(chan struct{})
	var received []Event
	var mu sync.Mutex
	var once sync.Once

	// Slow subscriber that intentionally starves the pumpEvents consumer
	bus.Subscribe(func(e Event) {
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
	bus := NewSimpleEventBus()
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	block := make(chan struct{})
	firstEvent := make(chan struct{})
	var once sync.Once
	bus.Subscribe(func(e Event) {
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

	err := bus.Flush(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	close(block)
}

func TestSimpleEventBus_Publish_BufferFull(t *testing.T) {
	bus := NewSimpleEventBus()
	block := make(chan struct{})

	// defer 1: Will execute LAST. Triggers graceful shutdown.
	defer func() {
		_ = bus.Shutdown(context.Background())
	}()

	// defer 2: Will execute FIRST. Unblocks the subscriber.
	defer close(block)

	bus.Subscribe(func(e Event) {
		<-block
	})

	for i := 0; i < 1200; i++ {
		bus.Publish(i)
	}
}

func TestEventRingBuffer_Basic(t *testing.T) {
	buffer := &eventRingBuffer{max: 3}
	if buffer.len() != 0 {
		t.Errorf("Expected length 0, got %d", buffer.len())
	}

	buffer.push(1)
	buffer.push(2)

	if buffer.len() != 2 {
		t.Errorf("Expected length 2, got %d", buffer.len())
	}
	if val := buffer.front(); val != 1 {
		t.Errorf("Expected front to be 1, got %v", val)
	}
	if val := buffer.pop(); val != 1 {
		t.Errorf("Expected popped value to be 1, got %v", val)
	}
	if buffer.len() != 1 {
		t.Errorf("Expected length 1, got %d", buffer.len())
	}
}

func TestEventRingBuffer_Eviction(t *testing.T) {
	buffer := &eventRingBuffer{max: 3}
	buffer.push(1)
	buffer.push(2)
	buffer.push(3)
	
	// Buffer is now full [1, 2, 3]. Pushing next should evict 1.
	buffer.push(4) 

	if buffer.len() != 3 {
		t.Errorf("Expected length 3 after eviction, got %d", buffer.len())
	}
	if val := buffer.front(); val != 2 {
		t.Errorf("Expected front to be 2 after eviction, got %v", val)
	}
	if val := buffer.pop(); val != 2 {
		t.Errorf("Expected popped value to be 2, got %v", val)
	}
	
	// Buffer is now [3, 4]
	buffer.push(5)
	buffer.push(6) // Evicts 3
	
	if val := buffer.pop(); val != 4 {
		t.Errorf("Expected popped value to be 4, got %v", val)
	}
}

func TestEventRingBuffer_EmptyState(t *testing.T) {
	buffer := &eventRingBuffer{max: 3}
	if val := buffer.pop(); val != nil {
		t.Errorf("Expected nil when popping empty buffer, got %v", val)
	}
	if val := buffer.front(); val != nil {
		t.Errorf("Expected nil when fronting empty buffer, got %v", val)
	}
}

func TestSimpleEventBus_ConcurrentFlushAndShutdown(t *testing.T) {
	bus := NewSimpleEventBus()

	blockSub := make(chan struct{})
	flushStarted := make(chan struct{})
	bus.Subscribe(func(e Event) {
		if e == "flush_trigger" {
			close(flushStarted)
		}
		<-blockSub
	})

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Flush (will block on the subscriber channel)
	go func() {
		defer wg.Done()
		bus.Publish("flush_trigger")
		_ = bus.Flush(context.Background())
	}()

	// Ensure Flush has actually started and blocked before Shutdown is called
	<-flushStarted

	// Goroutine 2: Shutdown concurrently
	go func() {
		defer wg.Done()
		_ = bus.Shutdown(context.Background())
	}()

	// Ensure Shutdown has started and signaled via its internal closing channel.
	// Since we are in the same package, we can access private fields.
	<-bus.closing

	// Unblock the subscriber
	close(blockSub)

	wg.Wait()
	// Test passes if no panic occurs
}

func TestSimpleEventBus_Flush_EvictedFlushEvent(t *testing.T) {
	// Initialize a bus using NewSimpleEventBusWithCapacity(1).
	bus := NewSimpleEventBusWithCapacity(1)

	// Create a subscriber that blocks intentionally (so events queue up).
	block := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})

	ready := make(chan struct{})
	bus.Subscribe(func(e Event) {
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
	// It sends a flushEvent to the 'in' channel. 
	// pumpEvents will read it and push it to the ring buffer, evicting "in-buffer".
	flushErr := make(chan error, 1)
	go func() {
		flushErr <- bus.Flush(context.Background())
	}()

	// Give a tiny bit of time for pumpEvents to process the flushEvent
	time.Sleep(10 * time.Millisecond)

	// 4. Publish one more event to force the ring buffer to overflow and evict the flushEvent.
	bus.Publish("force-eviction")

	// 5. Assert that the blocked Flush() call returns ErrBufferOverflow.
	select {
	case err := <-flushErr:
		if !errors.Is(err, ErrBufferOverflow) {
			t.Errorf("expected ErrBufferOverflow, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Flush did not return ErrBufferOverflow")
	}
}

func TestSimpleEventBus_Flush_ConcurrentShutdown(t *testing.T) {
	bus := NewSimpleEventBus()

	// Block processing to ensure Flush blocks.
	block := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
	})
	ready := make(chan struct{})
	bus.Subscribe(func(e Event) {
		ready <- struct{}{}
		<-block
	})

	bus.Publish("init")
	<-ready

	flushErr := make(chan error, 1)
	go func() {
		flushErr <- bus.Flush(context.Background())
	}()

	// Give it a moment to enter the waiting state in Flush
	time.Sleep(10 * time.Millisecond)

	// Concurrently trigger bus.Shutdown().
	go func() {
		_ = bus.Shutdown(context.Background())
	}()

	// Assert that the Flush() call aborts and returns ErrBusClosed.
	select {
	case err := <-flushErr:
		if !errors.Is(err, ErrBusClosed) {
			t.Errorf("expected ErrBusClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Flush did not return after Shutdown")
	}
}

func TestNewSimpleEventBusWithCapacity_Defaults(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
	}{
		{"Zero capacity", 0},
		{"Negative capacity", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := NewSimpleEventBusWithCapacity(tt.capacity)
			if bus.capacity != DefaultMaxQueueSize {
				t.Errorf("expected capacity %d, got %d", DefaultMaxQueueSize, bus.capacity)
			}

			// Verify it doesn't panic on basic operations
			bus.Subscribe(func(e Event) {})
			bus.Publish("test")
			_ = bus.Flush(context.Background())
			_ = bus.Shutdown(context.Background())
		})
	}
}
