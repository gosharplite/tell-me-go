// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

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
			if bus.capacity != defaultMaxQueueSize {
				t.Errorf("expected capacity %d, got %d", defaultMaxQueueSize, bus.capacity)
			}

			// Verify it doesn't panic on basic operations
			bus.Subscribe(func(e Event) {})
			bus.Publish("test")
			_ = bus.Flush(context.Background())
			_ = bus.Shutdown(context.Background())
		})
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
		// Flush will wait for the blocked subscriber
		_ = bus.Flush(context.Background())
	}()

	// Ensure Flush has actually started and blocked before Shutdown is called
	<-flushStarted

	// Goroutine 2: Shutdown concurrently with a short timeout
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_ = bus.Shutdown(ctx)
	}()

	// Wait for Shutdown to timeout and call abort()
	select {
	case <-bus.closing:
		// Success: abort called
	case <-time.After(200 * time.Millisecond):
		t.Error("Shutdown did not call abort after timeout")
	}

	// Unblock the subscriber
	close(blockSub)

	wg.Wait()
}

func TestPumpEvents_ContextCancellation_LeakCheck(t *testing.T) {
	bus := NewSimpleEventBus()
	in := make(chan Event)
	out := make(chan Event)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.pumpEvents(ctx, in, out)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pumpEvents goroutine leaked after context cancellation")
	}
}

func TestPumpEvents_GracefulExit_OnInputClose(t *testing.T) {
	bus := NewSimpleEventBus()
	in := make(chan Event)
	out := make(chan Event)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.pumpEvents(ctx, in, out)
	}()

	// Signal graceful exit
	close(in)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pumpEvents goroutine leaked after input channel was closed")
	}
}

func TestPumpEvents_BlockedOnOut_ContextCancellation(t *testing.T) {
	bus := NewSimpleEventBusWithCapacity(1)
	in := make(chan Event)
	out := make(chan Event) // Unbuffered, no one reading
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.pumpEvents(ctx, in, out)
	}()

	// Send one event to put it in the buffer
	in <- "event1"
	// Wait a bit to ensure it's in the buffer and now trying to send to 'out'
	time.Sleep(20 * time.Millisecond)

	// Now pumpEvents should be blocked on 'out <- buffer.front()'

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pumpEvents goroutine leaked while blocked on out channel")
	}
}
