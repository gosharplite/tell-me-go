// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// mock_clock.go contains MockClock and the MockTicker type returned by
// MockClock.NewTicker. They are kept together because separating them
// would orphan a method receiver and force a circular import.

package agenttest

import (
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// MockClock is a test double for clock.Clock. When the stored time is
// non-zero (set via SetCurrentTime), Now() returns that value and Sleep()
// advances it; when zero, Now() falls through to the real wall clock.
// After() returns a buffered channel pre-loaded with the stored time+d so
// it never blocks. NewTicker() returns a *MockTicker fed from the
// same After() channel.
type MockClock struct {
	mu         sync.Mutex
	storedTime time.Time
}

// SetCurrentTime safely sets the fixed clock time for tests.
func (m *MockClock) SetCurrentTime(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storedTime = t
}

// currentTime returns the stored fixed time (race-safe).
// Returns the zero time.Time if never set.
func (m *MockClock) currentTime() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storedTime
}

func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	t := m.storedTime
	m.mu.Unlock()

	if t.IsZero() {
		return time.Now()
	}
	return t
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *MockClock) Sleep(d time.Duration) {
	m.mu.Lock()
	m.storedTime = m.storedTime.Add(d)
	m.mu.Unlock()
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	m.mu.Lock()
	t := m.storedTime.Add(d)
	m.mu.Unlock()

	c := make(chan time.Time, 1)
	c <- t
	return c
}

func (m *MockClock) NewTicker(d time.Duration) clock.Ticker {
	return &MockTicker{CVal: m.After(d)}
}

func (m *MockClock) Jitter(base float64) float64 {
	return base
}

// MockTicker is a test double for clock.Ticker. C() returns the channel
// supplied via CVal (typically wired up by MockClock.NewTicker); Stop
// is a no-op.
type MockTicker struct {
	CVal <-chan time.Time
}

func (m *MockTicker) C() <-chan time.Time {
	return m.CVal
}

func (m *MockTicker) Stop() {}
