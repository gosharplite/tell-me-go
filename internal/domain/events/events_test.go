package events

import (
	"context"
	"sync"
	"testing"
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
