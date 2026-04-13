// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// MockClock implements clock.Clock for testing.
type MockClock struct {
	CurrentTime time.Time
}

func (m *MockClock) Now() time.Time {
	if m.CurrentTime.IsZero() {
		return time.Now()
	}
	return m.CurrentTime
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *MockClock) Sleep(d time.Duration) {
	m.CurrentTime = m.CurrentTime.Add(d)
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- m.CurrentTime.Add(d)
	return c
}

func (m *MockClock) NewTicker(d time.Duration) clock.Ticker {
	return &MockTicker{CVal: m.After(d)}
}

func (m *MockClock) Jitter(base float64) float64 {
	return base
}

// MockTicker implements clock.Ticker for testing.
type MockTicker struct {
	CVal <-chan time.Time
}

func (m *MockTicker) C() <-chan time.Time {
	return m.CVal
}

func (m *MockTicker) Stop() {}
