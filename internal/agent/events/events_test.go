package events

import (
	"sync"
	"testing"
)

func TestSimpleEventBus_Race(t *testing.T) {
	bus := &SimpleEventBus{}
	wg := sync.WaitGroup{}
	
	numGoroutines := 100
	numEvents := 100
	
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
