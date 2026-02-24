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
				_ = bus.Publish(struct{}{})
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
		_ = bus.Publish(i)
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
		_ = bus.Publish(i)
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
	_ = bus.Publish("init")
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
	_ = bus.Publish("init")
	<-ready

	// Fill the buffer (capacity 100)
	for i := 0; i < 100; i++ {
		_ = bus.Publish(i)
	}

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
	_ = bus.Publish("init")
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

	_ = bus.Publish("event")
}

func TestSimpleEventBus_Publish_ClosedBus(t *testing.T) {
	bus := NewSimpleEventBus()
	_ = bus.Shutdown(context.Background())

	// Should return an error and not panic or block indefinitely
	err := bus.Publish("event")
	if err == nil {
		t.Error("expected error when publishing to closed bus, got nil")
	}
}

func TestSimpleEventBus_Flush_NoSubscribers(t *testing.T) {
	bus := NewSimpleEventBus()
	err := bus.Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil error when flushing with no subscribers, got %v", err)
	}
}

func TestSimpleEventBus_Publish_BufferFull(t *testing.T) {
	bus := NewSimpleEventBus()
	block := make(chan struct{})
	ready := make(chan struct{})
	bus.Subscribe(func(e Event) {
		ready <- struct{}{}
		<-block
	})
	_ = bus.Publish("init")
	<-ready

	// Fill the buffer (100)
	for i := 0; i < 100; i++ {
		err := bus.Publish(i)
		if err != nil {
			t.Errorf("unexpected error filling buffer: %v", err)
		}
	}

	// This should be dropped and return ErrBufferFull
	err := bus.Publish("dropped")
	if !errors.Is(err, ErrBufferFull) {
		t.Errorf("expected ErrBufferFull, got %v", err)
	}

	close(block)
}
