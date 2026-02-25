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
	bus.Subscribe(func(e Event) {
		<-blockSub 
	})

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Flush (will block on the subscriber channel)
	go func() {
		defer wg.Done()
		_ = bus.Flush(context.Background())
	}()

	// Ensure Flush has actually started and blocked before Shutdown is called
	time.Sleep(50 * time.Millisecond)

	// Goroutine 2: Shutdown concurrently
	go func() {
		defer wg.Done()
		_ = bus.Shutdown(context.Background())
	}()

	// Ensure Shutdown has invoked activeProducers.Wait()
	time.Sleep(50 * time.Millisecond)

	// Unblock the subscriber
	close(blockSub)
	
	wg.Wait()
	// Test passes if no panic occurs
}
