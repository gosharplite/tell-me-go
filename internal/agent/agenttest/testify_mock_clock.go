// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/mock"
)

// TestifyMockClock is a testify-based test double for clock.Clock. It
// is the more verbose alternative to MockClock for tests that need to
// assert exactly which clock methods were called and with what
// arguments. For tests that just need a fixed time source, prefer the
// simpler MockClock.
type TestifyMockClock struct {
	mock.Mock
}

func (m *TestifyMockClock) Now() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func (m *TestifyMockClock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *TestifyMockClock) Sleep(d time.Duration) {
	m.Called(d)
}

func (m *TestifyMockClock) After(d time.Duration) <-chan time.Time {
	args := m.Called(d)
	return args.Get(0).(<-chan time.Time)
}

func (m *TestifyMockClock) NewTicker(d time.Duration) clock.Ticker {
	args := m.Called(d)
	return args.Get(0).(clock.Ticker)
}

func (m *TestifyMockClock) Jitter(base float64) float64 {
	args := m.Called(base)
	return args.Get(0).(float64)
}
