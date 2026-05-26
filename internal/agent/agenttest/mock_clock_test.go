// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"
	"testing"
	"time"
)

// TestMockClock_RaceDetection verifies that concurrent writes to
// CalledMethods and CalledNow on MockClock do not trigger the race
// detector. This test is a precondition for adding sync.Mutex.
func TestMockClock_RaceDetection(t *testing.T) {
	m := &MockClock{}

	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 20

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Mix of all mutating methods
				m.Now()
				m.Sleep(1 * time.Millisecond)
				m.After(1 * time.Millisecond)
				m.NewTicker(1 * time.Millisecond)
				m.Jitter(1.0)
			}
		}()
	}
	wg.Wait()

	// CalledMethods should have goroutines*iterations*6 entries
	// (NewTicker also appends "After" via its internal call to After)
	expected := goroutines * iterations * 6
	if len(m.CalledMethods) != expected {
		t.Errorf("got %d entries, want %d", len(m.CalledMethods), expected)
	}
}
