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
	Since(t time.Time) time.Duration
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

func (RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// FakeClock is a test-only Clock that allows manual control of time.
// The zero value is ready to use with default behavior.
type FakeClock struct {
	// AfterChan is the channel returned by After. Tests send to it to
	// simulate elapsed time.
	AfterChan chan time.Time
	// SleepChan receives the duration passed to Sleep.
	SleepChan chan time.Duration
	// Ticker is the FakeTicker that NewTicker returns. Tests call
	// Ticker.Fire() to simulate a tick.
	Ticker *FakeTicker
}

func (f *FakeClock) Now() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func (f *FakeClock) Since(t time.Time) time.Duration {
	return f.Now().Sub(t)
}

func (f *FakeClock) Sleep(d time.Duration) {
	if f.SleepChan != nil {
		f.SleepChan <- d
	}
}

func (f *FakeClock) After(d time.Duration) <-chan time.Time {
	return f.AfterChan
}

func (f *FakeClock) NewTicker(d time.Duration) Ticker {
	if f.Ticker == nil {
		f.Ticker = NewFakeTicker()
	}
	return f.Ticker
}

func (f *FakeClock) Jitter(base float64) float64 {
	return base // deterministic: no jitter
}

// FakeTicker is a test-only Ticker. Tests call Fire() to emit a
// time value on the tick channel.
type FakeTicker struct {
	CChan chan time.Time
}

func NewFakeTicker() *FakeTicker {
	return &FakeTicker{CChan: make(chan time.Time, 1)}
}

func (ft *FakeTicker) C() <-chan time.Time {
	return ft.CChan
}

func (ft *FakeTicker) Fire() {
	select {
	case ft.CChan <- time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC):
	default:
	}
}

func (ft *FakeTicker) Stop() {}
