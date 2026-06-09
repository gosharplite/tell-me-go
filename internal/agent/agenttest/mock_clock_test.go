// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"
	"testing"
	"time"
)

// TestMockClock_RaceDetection verifies that concurrent calls to
// Now() and SetCurrentTime do not trigger the race detector.
// The test relies on -race for detection.
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
				m.Now()
				m.SetCurrentTime(time.Now())
				m.Sleep(1 * time.Millisecond)
				m.After(1 * time.Millisecond)
				m.NewTicker(1 * time.Millisecond)
				m.Jitter(1.0)
			}
		}()
	}
	wg.Wait()
}
