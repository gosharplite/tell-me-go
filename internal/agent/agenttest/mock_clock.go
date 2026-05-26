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

// MockClock is a test double for clock.Clock. CurrentTime, when
// non-zero, fixes the value returned by Now() and is advanced by
// Sleep(); when zero, Now() falls through to the real wall clock.
// After() returns a buffered channel pre-loaded with CurrentTime+d so
// it never blocks. NewTicker() returns a *MockTicker fed from the
// same After() channel.
type MockClock struct {
	mu            sync.Mutex
	CurrentTime   time.Time
	CalledNow     int
	CalledMethods []string
}

func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	m.CalledNow++
	m.CalledMethods = append(m.CalledMethods, "Now")
	t := m.CurrentTime
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
	m.CalledMethods = append(m.CalledMethods, "Sleep")
	m.CurrentTime = m.CurrentTime.Add(d)
	m.mu.Unlock()
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	m.mu.Lock()
	m.CalledMethods = append(m.CalledMethods, "After")
	t := m.CurrentTime.Add(d)
	m.mu.Unlock()

	c := make(chan time.Time, 1)
	c <- t
	return c
}

func (m *MockClock) NewTicker(d time.Duration) clock.Ticker {
	m.mu.Lock()
	m.CalledMethods = append(m.CalledMethods, "NewTicker")
	m.mu.Unlock()

	return &MockTicker{CVal: m.After(d)}
}

func (m *MockClock) Jitter(base float64) float64 {
	m.mu.Lock()
	m.CalledMethods = append(m.CalledMethods, "Jitter")
	m.mu.Unlock()

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
