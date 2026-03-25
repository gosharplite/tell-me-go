// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clock

import (
	"math/rand/v2"
	"time"
)

// Clock provides a way to get the current time and handle delays, facilitating deterministic testing.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) Ticker
	Jitter(base float64) float64
}

// Ticker provides a testable interface for a time ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// RealClock implements the Clock interface using the standard time package.
type RealClock struct{}

// Now returns the current local time.
func (RealClock) Now() time.Time {
	return time.Now()
}

// Sleep pauses the current goroutine for at least the duration d.
func (RealClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

// After waits for the duration to elapse and then sends the current time on the returned channel.
func (RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// NewTicker returns a new Ticker that will send the current time on its channel after each tick.
func (RealClock) NewTicker(d time.Duration) Ticker {
	return realTicker{time.NewTicker(d)}
}

// Jitter returns the base value with some random jitter applied (+/- 10%).
func (RealClock) Jitter(base float64) float64 {
	// math/rand/v2 is used for better performance and better API
	return base * (0.9 + (rand.Float64() * 0.2))
}

type realTicker struct {
	*time.Ticker
}

func (rt realTicker) C() <-chan time.Time {
	return rt.Ticker.C
}

