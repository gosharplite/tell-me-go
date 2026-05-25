// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// mock_clock.go contains MockClock and the MockTicker type returned by
// MockClock.NewTicker. They are kept together because separating them
// would orphan a method receiver and force a circular import.

package agenttest

import (
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
	CurrentTime   time.Time
	CalledNow     int
	CalledMethods []string
}

func (m *MockClock) Reset() {
	m.CalledNow = 0
	m.CalledMethods = nil
}

func (m *MockClock) Now() time.Time {
	m.CalledNow++
	m.CalledMethods = append(m.CalledMethods, "Now")
	if m.CurrentTime.IsZero() {
		return time.Now()
	}
	return m.CurrentTime
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *MockClock) Sleep(d time.Duration) {
	m.CalledMethods = append(m.CalledMethods, "Sleep")
	m.CurrentTime = m.CurrentTime.Add(d)
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	m.CalledMethods = append(m.CalledMethods, "After")
	c := make(chan time.Time, 1)
	c <- m.CurrentTime.Add(d)
	return c
}

func (m *MockClock) NewTicker(d time.Duration) clock.Ticker {
	m.CalledMethods = append(m.CalledMethods, "NewTicker")
	return &MockTicker{CVal: m.After(d)}
}

func (m *MockClock) Jitter(base float64) float64 {
	m.CalledMethods = append(m.CalledMethods, "Jitter")
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
